package servers

// Ollama setup is a machine operation. Termux packages supply the Android binary
// and either Android's vendor Vulkan loader or Mesa's Turnip driver for Adreno.
// Installing a driver does not establish that a model computes correctly on it.
import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"ai-control-mesh/machine/internal/process"
	"ai-control-mesh/machine/internal/settings"
)

const OllamaAptEnv = "AI_PROXY_OLLAMA_APT"

var ollamaVariants = []Variant{
	{ID: "cpu", Label: "CPU only", Help: "Most compatible. Disables GPU backends.", Recommended: true, packages: []string{"ollama"}},
	{ID: "vulkan", Label: "Vulkan (Android driver)", Help: "Experimental phone GPU support using Android's driver. Replaces an existing generic Vulkan loader.", packages: []string{"ollama", "vulkan-loader-android"}},
	{ID: "vulkan-adreno", Label: "Vulkan (Adreno / Turnip)", Help: "Experimental Snapdragon GPU support using Mesa Turnip. Replaces Android's Vulkan loader. Some devices or models produce incorrect replies; test before relying on it.", packages: []string{"ollama", "vulkan-loader-generic", "mesa-vulkan-icd-freedreno"}},
}

var ollamaJobMu sync.Mutex
var ollamaJob *installJob

func ollamaInstallOptions() map[string]any {
	if termuxPrefix() == "" {
		return map[string]any{"variants": []Variant{}, "error": "Automatic Ollama installation is available in Termux. On this platform install Ollama from https://ollama.com/download, then configure it here."}
	}
	return map[string]any{"source": "termux", "variants": ollamaVariants}
}

func ollamaInstallProgress() any {
	ollamaJobMu.Lock()
	defer ollamaJobMu.Unlock()
	if ollamaJob == nil {
		return nil
	}
	copy := *ollamaJob
	return copy
}

func ollamaInstalling() bool {
	ollamaJobMu.Lock()
	defer ollamaJobMu.Unlock()
	return ollamaJob != nil && ollamaJob.State == "installing"
}

func ollamaInstallLog() string { return filepath.Join(settings.Home(), "ollama-install.log") }

func (m *Manager) startOllamaInstall(variant string) error {
	if termuxPrefix() == "" {
		return errors.New("automatic Ollama installation requires Termux")
	}
	var chosen *Variant
	for i := range ollamaVariants {
		if ollamaVariants[i].ID == variant {
			chosen = &ollamaVariants[i]
		}
	}
	if chosen == nil {
		return errors.New("pick an Ollama install option: cpu, vulkan or vulkan-adreno")
	}
	if m.ollamaVersion() != "" || managedPID() > 0 {
		return errors.New("stop Ollama before installing or changing its GPU setup")
	}
	ollamaJobMu.Lock()
	if ollamaJob != nil && ollamaJob.State == "installing" {
		ollamaJobMu.Unlock()
		return errors.New("Ollama is already being installed here")
	}
	ollamaJob = &installJob{State: "installing", Variant: variant, Step: "starting", At: time.Now().UTC().Format(time.RFC3339)}
	ollamaJobMu.Unlock()
	go func() {
		err := m.installOllamaTermux(*chosen)
		ollamaJobMu.Lock()
		defer ollamaJobMu.Unlock()
		ollamaJob.At = time.Now().UTC().Format(time.RFC3339)
		if err != nil {
			ollamaJob.State, ollamaJob.Error = "failed", err.Error()
		} else {
			ollamaJob.State, ollamaJob.Step = "installed", "Configured. Start Ollama to use these settings; GPU inference has not been verified."
			if chosen.ID == "cpu" {
				ollamaJob.Step = "Configured for CPU. Start Ollama to use these settings."
			}
		}
	}()
	return nil
}

func (m *Manager) installOllamaTermux(chosen Variant) error {
	apt := command(OllamaAptEnv, func() string { return filepath.Join(termuxPrefix(), "bin", "apt-get") })
	if apt == nil {
		return errors.New("Termux's apt-get isn't here")
	}
	if err := os.MkdirAll(settings.Home(), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(ollamaInstallLog(), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	for _, args := range [][]string{{"update"}, append([]string{"install", "-y", "--no-install-recommends", "-o", "Dpkg::Options::=--force-confnew"}, chosen.packages...)} {
		ollamaJobMu.Lock()
		ollamaJob.Step = "apt-get " + args[0]
		ollamaJobMu.Unlock()
		fmt.Fprintln(file, "# apt-get "+strings.Join(args, " "))
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
		cmd := process.CommandContext(ctx, apt[0], append(append([]string(nil), apt[1:]...), args...)...)
		cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
		cmd.Stdout, cmd.Stderr = file, file
		process.Quiet(cmd)
		err = cmd.Run()
		cancel()
		if err != nil {
			return fmt.Errorf("apt-get %s: %v (see %s): %s", args[0], err, ollamaInstallLog(), tail(ollamaInstallLog(), 1200))
		}
	}
	prefix := termuxPrefix()
	paths := []string{filepath.Join(prefix, "bin", "ollama")}
	if chosen.ID != "cpu" {
		paths = append(paths, filepath.Join(prefix, "lib", "ollama", "vulkan", "libggml-vulkan.so"), filepath.Join(prefix, "lib", "libvulkan.so"))
	}
	for _, path := range paths {
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("packages installed, but required file %s is missing; settings were not changed", path)
		}
	}
	// Don't overwrite a user's models directory or unrelated tuning. Apply the
	// hardware profile only once apt and the backend files have succeeded.
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.machine().Servers {
		return errors.New("server management was switched off; settings were not changed")
	}
	if m.ollamaVersion() != "" || managedPID() > 0 {
		return errors.New("Ollama was started during installation; stop it and run setup again to apply the hardware settings")
	}
	config := ollamaSetupConfig(LoadConfig(), chosen.ID)
	_, err = SaveConfig(config)
	return err
}

func ollamaSetupConfig(config map[string]any, variant string) map[string]any {
	config["vulkan"], config["gpus"], config["llm_library"] = variant != "cpu", "", ""
	if variant == "cpu" {
		config["gpus"], config["llm_library"] = "-1", "cpu"
	} else {
		config["kv_cache_type"], config["flash_attention"] = "f16", false
		config["context_length"], config["num_parallel"], config["max_loaded_models"], config["new_engine"] = int64(8192), int64(1), int64(1), false
	}
	// Extra environment values override the form. Remove profile overrides so a
	// previous OLLAMA_VULKAN=0 cannot silently defeat the selected GPU setup.
	profile := map[string]bool{"OLLAMA_VULKAN": true, "OLLAMA_LLM_LIBRARY": true}
	if variant != "cpu" {
		for _, name := range []string{"OLLAMA_KV_CACHE_TYPE", "OLLAMA_FLASH_ATTENTION", "OLLAMA_CONTEXT_LENGTH", "OLLAMA_NUM_PARALLEL", "OLLAMA_MAX_LOADED_MODELS", "OLLAMA_NEW_ENGINE"} {
			profile[name] = true
		}
	}
	var lines []string
	for _, line := range strings.Split(fmt.Sprint(config["extra_env"]), "\n") {
		name, _, _ := strings.Cut(strings.TrimSpace(line), "=")
		if !profile[strings.TrimSpace(name)] {
			lines = append(lines, line)
		}
	}
	config["extra_env"] = strings.Join(lines, "\n")
	return config
}
