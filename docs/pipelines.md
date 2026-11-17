# Jenkins builds and verification

The Jenkins definitions are versioned in `pipelines/`. Jenkins reads them from
this GitHub repository. Build and verification use multibranch jobs, so each
discovered branch has its own workspace, history and status. Both use
`checkout scm` to test or build the exact revision selected by Jenkins.

## Jobs and triggers

| Job | Behavior |
| --- | --- |
| `ai-control-mesh-verify` | Discovers branches and open PRs, including forks; runs Go unit tests, server tests, UI typechecking and the UI build. Reports `jenkins/branch-verification` on GitHub. |
| `ai-control-mesh` | Discovers repository branches and builds a complete versioned distribution. Only `master` builds automatically; use Build Now for another branch. PRs use verification instead. |
| `ai-control-mesh-test` | Deploys one exact distribution with Compose and runs smoke tests. Successful branch builds queue this job when `RUN_DEPLOYMENT_TESTS` is enabled. |
| `ai-control-mesh-distribute` | Explicitly promotes one tested distribution to `pi`, `github`, or `both`. No automatic release trigger. |

Multibranch indexing runs every five minutes. It finds new branches, PRs and
commits without a public webhook. Branches with an open origin PR are verified
through their PR job to avoid duplicate verification. PR jobs check the PR head,
rather than a synthetic merge. Fork PRs use the target branch's trusted pipeline
definition; changes to a fork's Jenkinsfile do not execute on the host.
Branches need the corresponding `pipelines/Jenkinsfile.*` file to be discovered.
The target branch must contain the verification definition before fork PR jobs
can use it. Existing branches created before these files were added must merge
or rebase onto a revision containing them.

Distribution testing and release stay parameterized because they consume an
already-built version. They load their definitions from `master`. During rollout,
the bootstrap accepts the branch carrying these definitions as an argument;
rerun it without that argument after merging to return operations to `master`.

## Agent and controller setup

Use your Jenkins controller URL, such as `https://ci.example.com/`. Builds use
the `lan-builder` agent. Install Pipeline, Git and GitHub Branch Source plugins.
Create a Jenkins username/password credential named `github-ci`, with the GitHub
username and token. It supplies repository discovery and SCM checkout; keep it
in Jenkins credentials, never in Git. Host-side commit status and release
commands use the agent's existing `gh` authentication.

The pipeline definitions call the installed CI runners through `MESH_CI_HOME`.
Set that agent environment variable to your CI installation directory, such as
`/srv/mesh-ci`. Also set `MESH_DOWNLOADS_URL` to the public artifact base URL,
such as `https://downloads.example.com`. The bootstrap requires both settings
and preserves their configured values. The CI installation directory
provides `build.py`, `verify.py`, `test_distribution.py`,
`release_distribution.py`, `jenkins/github-status.py` and
`jenkins/collect-results.py`, together with their supporting configuration.
Changing a Jenkinsfile does not replace those installed runners. Host secrets,
toolchains, caches and distribution storage remain outside the Git checkout.

The agent needs Python, Go, Node/npm, Podman, podman-compose, the Android SDK and
Java for complete distributions. Install Jenkins' JUnit plugin for test reporting.
The Android runtime stage also requires the NDK, build tools, the Android 36
platform, the Android 36.1 Google Play x86-64 system image, the emulator and KVM.
Set `ANDROID_HOME` and `JAVA_HOME`; install the NDK under the SDK's `ndk/` directory
or set `ANDROID_NDK_HOME`. Missing tooling fails the stage rather than skipping it.
Verification runs source in disposable
containers without deployment credentials or host service sockets. The build
and release runners require the existing trusted host installation and Pi
connection configuration.

Run the bootstrap with an administrator's Jenkins CLI:

```sh
java -jar jenkins-cli.jar -s https://ci.example.com/ -webSocket groovy = < pipelines/configure.groovy
```

For an initial rollout from a feature branch, append its name after `groovy =`.
The bootstrap archives old standalone build and verification jobs as
`ai-control-mesh-legacy` and `ai-control-mesh-verify-legacy`, disables them, and
preserves their build records. It creates or refreshes the two multibranch jobs,
updates the two operations jobs to Pipeline from SCM, and indexes branches.
It refuses to migrate a running job or overwrite an existing legacy backup.
Run it again after merging to update the operations jobs to `master`.

## Distributions and promotion

Every distribution build runs **Android runtime regressions** after packaging.
It creates a fresh emulator, private ADB server and disposable test APK, then
executes the Go regressions as an Android app user. This reproduces Android's
app-storage execution restrictions, which an ordinary Linux Go test cannot test.
The required checks cover native commands, scripts, literal arguments, environment
and working directory, cancellation, PTY input/output, the machine launching the
agent for a console shell, Ollama and llama.cpp startup/shutdown, correct child executable identity despite a stale Termux parent override, and ARM64 loader/DNS linkage.

Branch/PR verification includes **Ollama setup Python parity**. It runs the Python install tests and the same real fleet/server install contract used by the Go server suite, in a disposable container. The machine unit stage covers CPU, Android Vulkan and Turnip package selection, environment overrides, failure preservation, concurrent changes and refusing setup while Ollama runs. No test installs host packages or requires GPU hardware; real model correctness on a GPU still needs a hardware check.

Jenkins publishes JUnit results and archives logs in that stage. The distribution
also contains the checksummed `android-runtime/result.json`, JUnit and logs.
Build finalization and release require a successful result for the exact commit,
with no skipped checks. A failed retry invalidates the earlier result. This stage
belongs to trusted origin builds; fork PR verification retains its container
isolation and does not receive access to host Android tools or devices.

The shared host directory is `MESH_CI_HOME/dist/<version>/`, unless the installed
runners' `DIST_ROOT` environment setting selects another disk location. The
version includes the source commit and UTC timestamp. Each distribution contains
`build.json`, `SHA256SUMS`, platform downloads, installers and AMD64/ARM64 server
image archives. The Jenkins build archives its receipt and download link.

The deployment test loads the archived AMD64 image and starts an isolated
PostgreSQL, issuer and server stack. The versioned framework in
`pipelines/integration/` is copied into the distribution and covered by its
manifest. Seven checks cover service health, the exact version and downloads,
unauthenticated access rejection, real OAuth device sign-in, the packaged Go
machine joining the fleet, a folders RPC reaching that machine, and rejection
of paths outside its configured roots. These checks use disposable credentials
and private container networking. Logs and results are archived in the test job;
containers and volumes are removed after the run.

Release requires a successful deployment-test result matching the source commit
and distribution manifest, with no failed or skipped deployment checks. Pi
releases reuse the existing backup and health checks; GitHub releases upload the
distribution archive and standalone installer files as a prerelease. Neither
operation rebuilds source. Builds and verification never trigger production
deployment or GitHub publication.

To deploy, open **Build with Parameters** on `ai-control-mesh-distribute` and
choose **VERSION** from the dropdown, then select **TARGET**. Available versions
have passed deployment testing and are ordered newest first. The initial
`Select a tested distribution` entry requires an explicit selection. The release
runner rechecks all promotion gates, including branch verification, before deployment.
The dropdown refreshes after every deployment-test run and when jobs are installed.
For a manual refresh, run `python3 pipelines/refresh-versions.py` from the source
checkout on the trusted CI host with `MESH_CI_HOME` set. Removed distributions and
versions whose test results no longer match disappear on the next refresh.
