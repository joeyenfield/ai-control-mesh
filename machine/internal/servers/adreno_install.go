package servers

// The stock Termux ICD cannot expose Qualcomm OpenCL on some Samsung phones.
// Build the pinned, tested generic backend with the MIT vendor-namespace loader.
import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"ai-control-mesh/machine/internal/process"
)

const (
	AdrenoSourceEnv  = "AI_PROXY_LLAMACPP_ADRENO_SOURCE"
	AdrenoBuilderEnv = "AI_PROXY_LLAMACPP_ADRENO_BUILDER"
	adrenoCommit     = "e358d59178377be4c58ba567925e05faadbccb57"
	adrenoSourceSHA  = "bb1c3b0467180a700ae2b27f067b126e22700c860afbfb7900cfaf9a4738b3a2"
	adrenoPatchSHA   = "dc21507d755969fbfad95b6493a78ffdb30892a52a5d26cb1f10dc2a7af4a483"
)

//go:embed adreno/loader.patch
var adrenoPatch []byte

//go:embed adreno/LICENSE
var adrenoLicense []byte

//go:embed adreno/build.py
var adrenoBuilder []byte

var adrenoVariant = Variant{ID: "opencl-adreno", Label: "OpenCL (Adreno, source build)", Help: "Qualcomm vendor-driver access, tested on Samsung S24 Ultra. Builds a pinned version on this phone; the first install can take several minutes. Applies an 8K context and F16 cache. Test a model after restarting."}

func adrenoAvailable() bool {
	if os.Getenv(AdrenoSourceEnv) != "" && os.Getenv(AdrenoBuilderEnv) != "" {
		return true
	} // explicit local test fixtures
	if runtime.GOARCH != "arm64" {
		return false
	}
	for _, path := range []string{"/vendor/lib64/libOpenCL.so", "/vendor/lib64/libOpenCL_adreno.so"} {
		if _, err := os.Stat(path); err != nil {
			return false
		}
	}
	return true
}

func adrenoOptions() []Variant {
	variants := append([]Variant(nil), termuxVariants...)
	for i := range variants {
		variants[i].Recommended = variants[i].ID == "cpu"
	}
	if adrenoAvailable() {
		chosen := adrenoVariant
		chosen.Recommended = true
		for i := range variants {
			variants[i].Recommended = false
		}
		variants = append([]Variant{chosen}, variants...)
	}
	return variants
}

func bytesSHA(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

type adrenoManifest struct{ Source, Patch, Recipe, Binary, Launcher string }

func installAdreno() error {
	if !adrenoAvailable() {
		return errors.New("this build requires ARM64 Termux and Qualcomm's vendor OpenCL driver")
	}
	if bytesSHA(adrenoPatch) != adrenoPatchSHA {
		return errors.New("embedded Adreno patch checksum mismatch")
	}
	if err := os.MkdirAll(installRoot(), 0o700); err != nil {
		return err
	}
	final := filepath.Join(installRoot(), "adreno-"+adrenoCommit[:12]+"-"+bytesSHA(adrenoBuilder)[:12])
	srcAsset := asset{Name: "adreno-source.tar.gz", URL: "https://codeload.github.com/ggml-org/llama.cpp/tar.gz/" + adrenoCommit, Digest: "sha256:" + adrenoSourceSHA}
	if override := os.Getenv(AdrenoSourceEnv); override != "" {
		if err := json.Unmarshal([]byte(override), &srcAsset); err != nil {
			return fmt.Errorf("invalid Adreno source override: %w", err)
		}
		srcAsset.Name = "adreno-source.tar.gz"
	}
	manifest := adrenoManifest{Source: strings.TrimPrefix(srcAsset.Digest, "sha256:"), Patch: adrenoPatchSHA, Recipe: bytesSHA(adrenoBuilder)}
	var cached adrenoManifest
	data, _ := os.ReadFile(filepath.Join(final, "build.json"))
	_ = json.Unmarshal(data, &cached)
	native := filepath.Join(final, "native", "llama-server")
	launcher := filepath.Join(final, "llama-server")
	nativeData, nativeErr := os.ReadFile(native)
	launcherData, launcherErr := os.ReadFile(launcher)
	valid := cached.Source == manifest.Source && cached.Patch == manifest.Patch && cached.Recipe == manifest.Recipe && nativeErr == nil && launcherErr == nil && cached.Binary == bytesSHA(nativeData) && cached.Launcher == bytesSHA(launcherData)
	if !valid {
		if _, err := os.Stat(final); err == nil {
			final += "-" + time.Now().UTC().Format("20060102T150405.000000000")
		}
		work, err := os.MkdirTemp(installRoot(), ".adreno-build-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(work)
		setJob(func(j *installJob) { j.Step = "installing source-build tools"; j.Release = adrenoCommit[:12] })
		apt := command(AptEnv, func() string { return filepath.Join(termuxPrefix(), "bin", "apt-get") })
		if apt == nil {
			return errors.New("Termux's apt-get isn't here")
		}
		for _, args := range [][]string{{"update"}, {"install", "-y", "--no-install-recommends", "clang", "cmake", "ninja", "python", "patch", "opencl-headers", "openssl", "pkg-config"}} {
			if err := runLogged(20*time.Minute, apt, args); err != nil {
				return fmt.Errorf("installing Adreno build tools: %w (see %s)", err, installLog())
			}
		}
		setJob(func(j *installJob) { j.Step = "downloading verified llama.cpp source" })
		archive, err := download(srcAsset, func(n int64) { setJob(func(j *installJob) { j.Done = n }) })
		if err != nil {
			return err
		}
		defer os.Remove(archive)
		source := filepath.Join(work, "source")
		if err := unpack(archive, srcAsset.Name, source); err != nil {
			return err
		}
		roots, _ := filepath.Glob(filepath.Join(source, "*", "CMakeLists.txt"))
		if len(roots) != 1 {
			return errors.New("source archive does not contain one llama.cpp source tree")
		}
		patch := filepath.Join(work, "loader.patch")
		recipe := filepath.Join(work, "build.py")
		if err := os.WriteFile(patch, adrenoPatch, 0o600); err != nil {
			return err
		}
		if err := os.WriteFile(recipe, adrenoBuilder, 0o600); err != nil {
			return err
		}
		builder := command(AdrenoBuilderEnv, func() string { return filepath.Join(termuxPrefix(), "bin", "python") })
		if builder == nil {
			return errors.New("Python is required to build the Adreno backend")
		}
		// The fixture replaces the recipe runner; production always executes our embedded recipe.
		args := []string{recipe, "--source", filepath.Dir(roots[0]), "--build", filepath.Join(work, "build"), "--prefix", termuxPrefix(), "--patch", patch}
		setJob(func(j *installJob) { j.Step = "compiling Qualcomm OpenCL (first install takes several minutes)" })
		if err := runLogged(60*time.Minute, builder, args); err != nil {
			return fmt.Errorf("Adreno build failed: %w (see %s): %s", err, installLog(), tail(installLog(), 700))
		}
		installed := filepath.Join(work, "installed")
		native = filepath.Join(installed, "native", "llama-server")
		file, err := os.Open(filepath.Join(work, "build", "bin", "llama-server"))
		if err != nil {
			return err
		}
		_, err = writeFile(native, file, 0o700, 256<<20)
		file.Close()
		if err != nil {
			return err
		}
		binaryData, err := os.ReadFile(native)
		if err != nil {
			return err
		}
		manifest.Binary = bytesSHA(binaryData)
		realBinary := filepath.Join(final, "native", "llama-server")
		quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
		wrapper := []byte("#!" + filepath.Join(termuxPrefix(), "bin", "sh") + "\nexport TERMUX_EXEC__PROC_SELF_EXE=" + quote(realBinary) + "\nexec " + quote(realBinary) + " --no-warmup \"$@\"\n")
		manifest.Launcher = bytesSHA(wrapper)
		if err := os.WriteFile(filepath.Join(installed, "llama-server"), wrapper, 0o700); err != nil {
			return err
		}
		info, _ := json.MarshalIndent(manifest, "", "  ")
		for name, contents := range map[string][]byte{"build.json": info, "LICENSE.vendor-loader": adrenoLicense} {
			if err := os.WriteFile(filepath.Join(installed, name), contents, 0o600); err != nil {
				return err
			}
		}
		license, _ := os.ReadFile(filepath.Join(filepath.Dir(roots[0]), "LICENSE"))
		if len(license) > 0 {
			_ = os.WriteFile(filepath.Join(installed, "LICENSE.llama.cpp"), license, 0o600)
		}
		if err := os.Rename(installed, final); err != nil {
			return err
		}
	}
	setJob(func(j *installJob) { j.Step = "checking Qualcomm GPU access"; j.Release = adrenoCommit[:12] })
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cmd := process.CommandContext(ctx, filepath.Join(final, "native", "llama-server"), "--list-devices")
	cmd.Env = process.ExecutableEnvironment(cmd, LlamaEnvironment(os.Environ(), LlamaDefaultConfig()))
	process.Quiet(cmd)
	output, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(output), "GPUOpenCL") || !strings.Contains(strings.ToLower(string(output)), "adreno") {
		return fmt.Errorf("built backend cannot access the Adreno GPU; previous selection is unchanged: %v: %s", err, strings.TrimSpace(string(output)))
	}
	previous := loadInstall()
	old := LoadLlamaConfig()
	config := adrenoConfig(LoadLlamaConfig())
	if _, err := SaveLlamaConfig(config); err != nil {
		return err
	}
	record := installRecord{Source: "source", Release: adrenoCommit[:12], Variant: "opencl-adreno", Binary: filepath.Join(final, "llama-server"), Dir: final, InstalledAt: time.Now().UTC().Format(time.RFC3339)}
	if err := record.save(); err != nil {
		_, _ = SaveLlamaConfig(old)
		return err
	}
	prune(final, previous.Dir)
	return nil
}

func adrenoConfig(config map[string]any) map[string]any {
	for name, value := range map[string]any{"ctx_size": int64(8192), "cache_type_k": "f16", "cache_type_v": "f16", "flash_attn": "off", "cache_ram": int64(0), "parallel": int64(1), "models_max": int64(1), "threads": int64(4), "gpu_layers": "all", "device": "GPUOpenCL"} {
		config[name] = value
	}
	managed := map[string]bool{}
	for _, name := range []string{"CTX_SIZE", "CACHE_TYPE_K", "CACHE_TYPE_V", "FLASH_ATTN", "CACHE_RAM", "N_PARALLEL", "MODELS_MAX", "THREADS", "N_GPU_LAYERS", "DEVICE"} {
		managed["LLAMA_ARG_"+name] = true
	}
	var lines []string
	for _, line := range strings.Split(fmt.Sprint(config["extra_env"]), "\n") {
		name, _, _ := strings.Cut(strings.TrimSpace(line), "=")
		if !managed[strings.TrimSpace(name)] {
			lines = append(lines, line)
		}
	}
	config["extra_env"] = strings.Join(lines, "\n")
	return config
}
