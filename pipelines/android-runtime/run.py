"""Run native Go regressions inside a fresh Android app sandbox, producing JUnit."""
from __future__ import annotations

import argparse
from contextlib import ExitStack
import fcntl
import json
import os
from pathlib import Path
import re
import shlex
import socket
import subprocess
import tempfile
import time
import traceback
import xml.etree.ElementTree as ET
import zipfile

PACKAGE = "com.aicontrolmesh.ciruntime"
PROCESS_TESTS = (
    "TestCommandPreservesArgumentsDirectoryAndEnvironment",
    "TestCommandContextStillCancelsChild",
    "TestAndroidCommandRunsScriptsAndAppPrivateBinaries",
    "TestAndroidShebangUsesTermuxInterpreter",
)
CASES = (
    ("client-process", "client", "process", "TestCommand|TestAndroid", PROCESS_TESTS),
    ("machine-process", "machine", "process", "TestCommand|TestAndroid", PROCESS_TESTS),
    ("terminal", "client", "terminal", "TestPTYStartsShellInAppStorage", ("TestPTYStartsShellInAppStorage",)),
    ("presence", "machine", "presence", "TestShellLaunchStartsNativeAgent", ("TestShellLaunchStartsNativeAgent",)),
    ("ollama", "machine", "servers", "TestOllamaStartsNativeBinaryAndStopsIt", ("TestOllamaStartsNativeBinaryAndStopsIt",)),
)


def command(args: list[str], log, *, env: dict[str, str], cwd: Path | None = None, timeout: int = 180) -> None:
    print("+ " + shlex.join(map(str, args)), flush=True)
    subprocess.run(args, cwd=cwd, env=env, stdout=log, stderr=subprocess.STDOUT, check=True, timeout=timeout)


def emulator_port() -> int:
    for port in range(5556, 5680, 2):
        with ExitStack() as stack:
            try:
                for value in (port, port + 1):
                    server = stack.enter_context(socket.socket())
                    server.bind(("127.0.0.1", value))
                return port
            except OSError:
                continue
    raise RuntimeError("No free Android emulator port pair")


def run(source: Path, output: Path) -> dict:
    output.mkdir(parents=True, exist_ok=True)
    for name in ("result.json", "junit.xml"):
        (output / name).unlink(missing_ok=True)
    commit = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=source, text=True).strip()
    result = {"schema": 1, "commit": commit, "status": "failed", "platform": "android-amd64", "tests": {"tests_run": 0, "failures": 0, "errors": 0, "skipped": 0}}
    suite = ET.Element("testsuite", name="Android app sandbox")
    emulator = None
    adb: list[str] = []
    adb_server: list[str] = []
    env = {key: value for key, value in os.environ.items() if not key.startswith("AI_PROXY_")}
    for key in ("HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"):
        env.pop(key, None)
    try:
        sdk = Path(env.get("ANDROID_HOME", env.get("ANDROID_SDK_ROOT", ""))).resolve()
        java = Path(env["JAVA_HOME"]) / "bin"
        tools = sorted((sdk / "build-tools").glob("*/aapt2"))[-1].parent
        ndks = sorted((sdk / "ndk").glob("*"))
        ndk = Path(env["ANDROID_NDK_HOME"]) if env.get("ANDROID_NDK_HOME") else ndks[-1]
        env["ANDROID_NDK_HOME"] = str(ndk)
        cc = ndk / "toolchains/llvm/prebuilt/linux-x86_64/bin/x86_64-linux-android24-clang"
        image = sdk / "system-images/android-36.1/google_apis_playstore/x86_64"
        android_jar = sdk / "platforms/android-36/android.jar"
        for path in (cc, java / "javac", tools / "d8", sdk / "emulator/emulator", image / "system.img", android_jar):
            if not path.is_file():
                raise RuntimeError(f"Missing Android runtime test prerequisite: {path}")
        harness = source / "pipelines/android-runtime"
        # Serialize this host's CI emulators without touching developers' AVDs.
        with (Path(tempfile.gettempdir()) / "mesh-ci-android-runtime.lock").open("a") as lock, tempfile.TemporaryDirectory(prefix="mesh-android-runtime-") as temporary, (output / "build.log").open("w") as log:
            fcntl.flock(lock, fcntl.LOCK_EX)
            work = Path(temporary)
            native_env = {**env, "GOOS": "android", "GOARCH": "amd64", "CGO_ENABLED": "1", "CC": str(cc), "GOTOOLCHAIN": "local"}
            for name, module, package, _, _ in CASES:
                command(["go", "test", "-c", "-buildmode=pie", "-trimpath", "-o", str(work / name), "./internal/" + package], log, cwd=source / module, env=native_env)
            linker_test = "TestAndroidARM64BuildUsesAndroidLoader"
            linker_log = output / "arm64-linker.log"
            with linker_log.open("w") as stream:
                command(["go", "test", "-count=1", "-v", "./dist", "-run", "^" + linker_test + "$"], stream, cwd=source / "tools", env=env)
            text = linker_log.read_text()
            case = ET.SubElement(suite, "testcase", classname="tools.dist", name=linker_test)
            if "--- PASS: " + linker_test not in text or "--- SKIP:" in text:
                ET.SubElement(case, "failure", message="ARM64 loader/DNS check did not pass").text = text
                raise RuntimeError("ARM64 loader/DNS regression was skipped or failed")
            classes = work / "classes"
            classes.mkdir()
            command([str(java / "javac"), "-source", "8", "-target", "8", "-classpath", str(android_jar), "-d", str(classes), str(harness / "RuntimeTests.java")], log, env=env)
            command([str(tools / "d8"), "--lib", str(android_jar), "--min-api", "26", "--output", str(work), str(classes / "com/aicontrolmesh/ciruntime/RuntimeTests.class")], log, env=env)
            apk = work / "unsigned.apk"
            command([str(tools / "aapt2"), "link", "-o", str(apk), "--manifest", str(harness / "AndroidManifest.xml"), "-I", str(android_jar)], log, env=env)
            with zipfile.ZipFile(apk, "a") as archive:
                archive.write(work / "classes.dex", "classes.dex")
            key = work / "test.keystore"
            command([str(java / "keytool"), "-genkeypair", "-keystore", str(key), "-storepass", "ci-test-only", "-keypass", "ci-test-only", "-alias", "test", "-keyalg", "RSA", "-keysize", "2048", "-validity", "2", "-dname", "CN=CIAndroidRuntime", "-noprompt"], log, env=env)
            signed = work / "runtime.apk"
            command([str(tools / "apksigner"), "sign", "--ks", str(key), "--ks-pass", "pass:ci-test-only", "--out", str(signed), str(apk)], log, env=env)
            avds = work / "avds"
            avd = avds / "runtime.avd"
            avd.mkdir(parents=True)
            (avds / "runtime.ini").write_text(f"avd.ini.encoding=UTF-8\npath={avd}\ntarget=android-36.1\n")
            (avd / "config.ini").write_text("\n".join(("AvdId=runtime", "abi.type=x86_64", "hw.cpu.arch=x86_64", "hw.cpu.ncore=2", "hw.ramSize=2048", "hw.gpu.enabled=yes", "hw.gpu.mode=swiftshader_indirect", "disk.dataPartition.size=2G", f"image.sysdir.1={image}/", "tag.id=google_apis_playstore", "PlayStore.enabled=false")) + "\n")
            user_home = work / "android-user"
            user_home.mkdir()
            with socket.socket() as server_socket:
                server_socket.bind(("127.0.0.1", 0))
                adb_port = server_socket.getsockname()[1]
            emulator_env = {**env, "ANDROID_AVD_HOME": str(avds), "ANDROID_USER_HOME": str(user_home),
                            "ANDROID_ADB_SERVER_PORT": str(adb_port), "ADB_SERVER_SOCKET": f"tcp:127.0.0.1:{adb_port}",
                            "ADB_VENDOR_KEYS": str(user_home / "adbkey")}
            port = emulator_port()
            adb_server = [str(sdk / "platform-tools/adb"), "-P", str(adb_port)]
            command([str(sdk / "platform-tools/adb"), "keygen", str(user_home / "adbkey")], log, env=emulator_env)
            command([*adb_server, "start-server"], log, env=emulator_env)
            adb = [*adb_server, "-s", "emulator-" + str(port)]
            with (output / "emulator.log").open("w") as emulator_log:
                emulator = subprocess.Popen([str(sdk / "emulator/emulator"), "-avd", "runtime", "-port", str(port), "-no-window", "-no-snapshot", "-no-audio", "-no-boot-anim", "-wipe-data", "-gpu", "swiftshader_indirect"], env=emulator_env, stdout=emulator_log, stderr=subprocess.STDOUT)
                try:
                    deadline = time.monotonic() + 180
                    while True:
                        if emulator.poll() is not None:
                            raise RuntimeError("Android emulator exited before boot; see emulator.log")
                        boot = subprocess.run([*adb, "shell", "getprop", "sys.boot_completed"], env=env, capture_output=True, text=True, timeout=10)
                        if boot.returncode == 0 and boot.stdout.strip() == "1":
                            break
                        if time.monotonic() > deadline:
                            raise RuntimeError("Android emulator boot timed out")
                        time.sleep(2)
                    command([*adb, "install", "-t", str(signed)], log, env=env)
                    for name, _, _, _, _ in CASES:
                        script = "mkdir -p files; cat > " + shlex.quote("files/" + name)
                        with (work / name).open("rb") as binary:
                            subprocess.run([*adb, "shell", shlex.join(["run-as", PACKAGE, "sh", "-c", script])], env=env, stdin=binary, stdout=log, stderr=subprocess.STDOUT, check=True, timeout=30)
                        command([*adb, "shell", shlex.join(["run-as", PACKAGE, "chmod", "700", "files/" + name])], log, env=env)
                    command([*adb, "shell", "am", "start", "-n", PACKAGE + "/.RuntimeTests"], log, env=env)
                    deadline = time.monotonic() + 300
                    for name, _, _, _, expected in CASES:
                        while True:
                            answer = subprocess.run([*adb, "shell", "run-as", PACKAGE, "cat", "files/" + name + ".exit"], env=env, capture_output=True, text=True, timeout=10)
                            if answer.returncode == 0:
                                break
                            if time.monotonic() > deadline:
                                raise RuntimeError("Android runtime tests timed out: " + name)
                            time.sleep(1)
                        captured = subprocess.check_output([*adb, "shell", "run-as", PACKAGE, "cat", "files/" + name + ".log"], env=env, text=True, timeout=10)
                        (output / (name + ".log")).write_text(captured)
                        statuses = dict((test, status) for status, test in re.findall(r"^\s*--- (PASS|FAIL|SKIP): (\S+)", captured, re.MULTILINE))
                        code = int(answer.stdout.strip())
                        for test in expected:
                            case = ET.SubElement(suite, "testcase", classname=name, name=test)
                            if code != 0 or statuses.get(test) != "PASS":
                                ET.SubElement(case, "failure", message=f"Android app test failed or skipped (exit {code})").text = captured
                        print(f"Android {name}: exit {code}, {len(expected)} required tests", flush=True)
                finally:
                    try:
                        subprocess.run([*adb, "emu", "kill"], env=env, stdout=log, stderr=subprocess.STDOUT, timeout=15, check=False)
                    except (OSError, subprocess.SubprocessError):
                        pass
                    try:
                        emulator.wait(timeout=20)
                    except subprocess.TimeoutExpired:
                        emulator.kill()
                        emulator.wait(timeout=10)
        if any(case.find("failure") is not None for case in suite):
            raise RuntimeError("Android runtime regression failed; see JUnit and native logs")
        result["status"] = "passed"
    except Exception as error:
        case = ET.SubElement(suite, "testcase", classname="android-runtime", name="runtime-stage")
        ET.SubElement(case, "error", message=str(error)).text = traceback.format_exc()
        (output / "error.log").write_text(traceback.format_exc())
    finally:
        if emulator is not None and emulator.poll() is None:
            emulator.kill()
            emulator.wait(timeout=10)
        if adb_server:
            subprocess.run([*adb_server, "kill-server"], env=env, stdout=subprocess.DEVNULL,
                           stderr=subprocess.DEVNULL, timeout=10, check=False)
        tests = result["tests"]
        tests["tests_run"] = len(suite)
        tests["failures"] = sum(case.find("failure") is not None for case in suite)
        tests["errors"] = sum(case.find("error") is not None for case in suite)
        for key, value in (("tests", tests["tests_run"]), ("failures", tests["failures"]), ("errors", tests["errors"]), ("skipped", 0)):
            suite.set(key, str(value))
        ET.ElementTree(suite).write(output / "junit.xml", encoding="utf-8", xml_declaration=True)
        (output / "result.json").write_text(json.dumps(result, indent=2) + "\n")
    return result


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    report = run(args.source.resolve(), args.output.resolve())
    print(json.dumps(report, indent=2), flush=True)
    raise SystemExit(0 if report["status"] == "passed" else 1)
