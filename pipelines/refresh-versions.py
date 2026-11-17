"""Refresh the release job's native Jenkins choice parameter from tested distributions.

Run on the trusted CI host after installing jobs and after deployment tests.
The installed release runner still revalidates every promotion gate at execution.
"""
import argparse
import json
import os
from pathlib import Path
import subprocess
import sys

PLACEHOLDER = 'Select a tested distribution'


def available_versions(dist_root, resolve, tested):
    candidates = []
    if not dist_root.exists():
        return []
    for directory in dist_root.iterdir():
        if not directory.is_dir() or directory.is_symlink():
            continue
        try:
            receipt = resolve(directory.name)
            tested(receipt)
            # CI versions carry a fixed-width UTC timestamp independent of base version.
            timestamp = directory.name.split('+ci.')[1].split('.')[0]
            candidates.append((timestamp, directory.name))
        except (OSError, ValueError, KeyError, TypeError, IndexError, AttributeError):
            continue
    return [version for _, version in sorted(candidates, reverse=True)]


def refresh_script(versions):
    # JSON is also valid Groovy list/string syntax for these validated CI versions.
    choices = json.dumps([PLACEHOLDER, *versions])
    return '''import hudson.model.ChoiceParameterDefinition
import hudson.model.ParametersDefinitionProperty
import jenkins.model.Jenkins
import org.jenkinsci.plugins.pipeline.modeldefinition.actions.DeclarativeJobPropertyTrackerAction
def job = Jenkins.get().getItemByFullName('ai-control-mesh-distribute')
assert job != null : 'Install the distribution job first'
synchronized (job) {
    def existing = job.getProperty(ParametersDefinitionProperty)
    assert existing != null : 'Configure distribution parameters first'
    def parameters = existing.parameterDefinitions.findAll { it.name != 'VERSION' }
    parameters.add(0, new ChoiceParameterDefinition('VERSION', CHOICES as String[],
        'Select a deployment-tested distribution (newest first).'))
    // Transfer ownership from the old static Jenkinsfile parameters. Otherwise
    // Declarative removes them on the first run after their directive is removed.
    def tracker = job.getAction(DeclarativeJobPropertyTrackerAction)
    if (tracker?.parameters?.contains('VERSION')) {
        job.removeAction(tracker)
    }
    while (job.getProperty(ParametersDefinitionProperty) != null) {
        job.removeProperty(ParametersDefinitionProperty)
    }
    job.addProperty(new ParametersDefinitionProperty(parameters))
    job.save()
}
println('Updated distribution version dropdown')
'''.replace('CHOICES', choices)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--list', action='store_true', help='Print versions without changing Jenkins')
    args = parser.parse_args()
    ci_home = Path(os.environ['MESH_CI_HOME']).resolve()
    sys.path.insert(0, str(ci_home))
    from distribution import DIST_ROOT, resolve, tested
    versions = available_versions(DIST_ROOT, resolve, tested)
    if args.list:
        print(json.dumps(versions))
    else:
        subprocess.run([str(ci_home / 'ci'), 'groovy', '='],
                       input=refresh_script(versions), text=True, check=True)
        print(f'{len(versions)} deployment-tested distributions available')


if __name__ == '__main__':
    main()
