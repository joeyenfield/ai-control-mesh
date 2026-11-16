# Jenkins pipelines

| File | Jenkins job | Inputs |
| --- | --- | --- |
| `Jenkinsfile.verify` | `ai-control-mesh-verify`, multibranch | Branch or PR selected by Jenkins |
| `Jenkinsfile.build` | `ai-control-mesh`, multibranch | Branch selected by Jenkins, `RUN_DEPLOYMENT_TESTS` |
| `Jenkinsfile.test` | `ai-control-mesh-test`, Pipeline from SCM | Exact distribution `VERSION` |
| `Jenkinsfile.distribute` | `ai-control-mesh-distribute`, Pipeline from SCM | Exact tested `VERSION`, `TARGET`: `pi`, `github`, `both` |

See [Jenkins builds and verification](../docs/pipelines.md) for setup, triggers,
agent requirements, artifacts and promotion. `configure.groovy` installs the job
configuration; pipeline definitions themselves are read from Git.

`integration/` holds the real deployment and control mesh checks. Builds include
these fixtures in their checksummed distributions so testing uses the framework
from the built commit.
