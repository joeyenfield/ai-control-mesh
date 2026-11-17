package servers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func adrenoFixture(t *testing.T, badDigest bool) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Termux source installation")
	}
	t.Setenv("AI_PROXY_MACHINE_HOME", t.TempDir())
	t.Setenv("AI_PROXY_MACHINE_DATA", "")
	prefix := t.TempDir()
	t.Setenv("PREFIX", prefix)
	t.Setenv("TERMUX_VERSION", "test")
	t.Setenv(LlamaEnv, "[]")
	setCommand(t, AptEnv, fake(t, "fake_ollama_apt.py"))
	setCommand(t, AdrenoBuilderEnv, fake(t, "fake_adreno_builder.py"))
	data := buildArchive(t, map[string]string{"llama-source/CMakeLists.txt": "fixture", "llama-source/LICENSE": "MIT fixture"})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(data) }))
	t.Cleanup(srv.Close)
	t.Setenv(ReleasesEnv, srv.URL)
	digest := sha(data)
	if badDigest {
		digest = "sha256:" + strings.Repeat("0", 64)
	}
	value, _ := json.Marshal(asset{URL: srv.URL + "/source.tar.gz", Digest: digest})
	t.Setenv(AdrenoSourceEnv, string(value))
	return prefix
}

func TestAdrenoInstallChecksGPUAppliesProfileAndReusesVerifiedBuild(t *testing.T) {
	prefix := adrenoFixture(t, false)
	old, err := SaveLlamaConfig(map[string]any{"ctx_size": int64(65536), "models_dir": "/existing-models", "jinja": false, "extra_env": "LLAMA_ARG_CTX_SIZE=65536\nGGML_VK_VISIBLE_DEVICES=0"})
	if err != nil {
		t.Fatal(err)
	}
	options := adrenoOptions()
	if options[0].ID != "opencl-adreno" || !options[0].Recommended {
		t.Fatalf("options: %v", options)
	}
	for i := 0; i < 2; i++ {
		if err := installAdreno(); err != nil {
			t.Fatal(err)
		}
		r := loadInstall()
		if r.Source != "source" || r.Variant != "opencl-adreno" || !strings.HasPrefix(r.Binary, installRoot()) {
			t.Fatalf("record: %+v", r)
		}
		got := LoadLlamaConfig()
		if got["ctx_size"] != int64(8192) || got["device"] != "GPUOpenCL" || got["cache_type_v"] != "f16" || got["flash_attn"] != "off" || got["models_dir"] != old["models_dir"] || got["jinja"] != false || got["extra_env"] != "GGML_VK_VISIBLE_DEVICES=0" {
			t.Fatalf("profile: %v", got)
		}
		for _, name := range []string{"LICENSE.vendor-loader", "LICENSE.llama.cpp", "build.json"} {
			if _, err := os.Stat(filepath.Join(r.Dir, name)); err != nil {
				t.Fatal(err)
			}
		}
	}
	calls, _ := os.ReadFile(filepath.Join(prefix, "adreno-builds.jsonl"))
	if strings.Count(strings.TrimSpace(string(calls)), "\n") != 0 || len(calls) == 0 {
		t.Fatalf("recompiled cached build: %s", calls)
	}
	apt, _ := os.ReadFile(filepath.Join(prefix, "apt-calls.jsonl"))
	if strings.Count(string(apt), "\n") != 2 || !strings.Contains(string(apt), "opencl-headers") {
		t.Fatalf("apt: %s", apt)
	}
}

func TestAdrenoFailuresPreserveSelectionAndSettings(t *testing.T) {
	for _, failure := range []string{"checksum", "compile", "gpu"} {
		t.Run(failure, func(t *testing.T) {
			adrenoFixture(t, failure == "checksum")
			if failure == "compile" {
				t.Setenv("FAKE_ADRENO_BUILD_FAIL", "1")
			}
			if failure == "gpu" {
				t.Setenv("FAKE_ADRENO_NO_GPU", "1")
			}
			previous := installRecord{Source: "found", Binary: "/existing/llama-server", Variant: "cpu"}
			if err := previous.save(); err != nil {
				t.Fatal(err)
			}
			old, err := SaveLlamaConfig(map[string]any{"ctx_size": int64(32768)})
			if err != nil {
				t.Fatal(err)
			}
			if err := installAdreno(); err == nil {
				t.Fatal("accepted failed installation")
			}
			if loadInstall() != previous || !reflect.DeepEqual(LoadLlamaConfig(), old) {
				t.Fatal("failed install changed selection/settings")
			}
		})
	}
}

func TestAdrenoEmbeddedAssetsMatchPython(t *testing.T) {
	if bytesSHA(adrenoPatch) != adrenoPatchSHA {
		t.Fatal("patch hash")
	}
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "python", "agent_py", "machine", "adreno_builder.py"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.ReplaceAll(string(data), "\r\n", "\n") != string(adrenoBuilder) {
		t.Fatal("Go and Python build recipes differ")
	}
}
