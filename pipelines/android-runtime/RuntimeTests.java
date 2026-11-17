package com.aicontrolmesh.ciruntime;

import android.app.Activity;
import android.os.Bundle;
import java.io.File;
import java.io.FileOutputStream;
import java.util.concurrent.TimeUnit;

public class RuntimeTests extends Activity {
    @Override public void onCreate(Bundle state) {
        super.onCreate(state);
        new Thread(() -> {
            File root = getFilesDir();
            String[][] tests = {
                {"client-process", "TestCommand|TestAndroid"},
                {"machine-process", "TestCommand|TestAndroid"},
                {"terminal", "TestPTYStartsShellInAppStorage"},
                {"presence", "TestShellLaunchStartsNativeAgent"},
                {"ollama", "TestOllamaStartsNativeBinaryAndStopsIt"}
            };
            for (String[] test : tests) {
                String name = test[0];
                try {
                    ProcessBuilder builder = new ProcessBuilder("/system/bin/linker64", new File(root, name).getAbsolutePath(), "-test.run", test[1], "-test.v");
                    builder.environment().put("HOME", root.getAbsolutePath());
                    builder.environment().put("TMPDIR", root.getAbsolutePath());
                    builder.redirectErrorStream(true);
                    builder.redirectOutput(new File(root, name + ".log"));
                    Process process = builder.start();
                    int code;
                    if (process.waitFor(120, TimeUnit.SECONDS)) {
                        code = process.exitValue();
                    } else {
                        process.destroyForcibly();
                        code = 124;
                    }
                    write(root, name + ".exit", Integer.toString(code));
                } catch (Exception error) {
                    try {
                        write(root, name + ".log", error.toString());
                        write(root, name + ".exit", "125");
                    } catch (Exception ignored) {}
                }
            }
        }).start();
    }

    private static void write(File root, String name, String value) throws Exception {
        try (FileOutputStream output = new FileOutputStream(new File(root, name))) {
            output.write(value.getBytes("UTF-8"));
        }
    }
}
