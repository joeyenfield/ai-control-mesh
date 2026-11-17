// Administrator bootstrap: run with the Jenkins CLI's `groovy =` command.
// Pipeline code is loaded from Git; host runners and credentials stay on the agent.
import hudson.model.BooleanParameterDefinition
import hudson.model.ChoiceParameterDefinition
import hudson.model.ParametersDefinitionProperty
import hudson.model.StringParameterDefinition
import hudson.plugins.git.GitSCM
import hudson.plugins.git.UserRemoteConfig
import hudson.plugins.git.BranchSpec
import hudson.slaves.EnvironmentVariablesNodeProperty
import jenkins.branch.BranchSource
import jenkins.branch.BranchProperty
import jenkins.branch.DefaultBranchPropertyStrategy
import jenkins.branch.NoTriggerBranchProperty
import jenkins.model.Jenkins
import org.jenkinsci.plugins.github_branch_source.BranchDiscoveryTrait
import org.jenkinsci.plugins.github_branch_source.ForkPullRequestDiscoveryTrait
import org.jenkinsci.plugins.github_branch_source.GitHubSCMSource
import org.jenkinsci.plugins.github_branch_source.OriginPullRequestDiscoveryTrait
import org.jenkinsci.plugins.workflow.cps.CpsScmFlowDefinition
import org.jenkinsci.plugins.workflow.job.WorkflowJob
import org.jenkinsci.plugins.workflow.multibranch.WorkflowBranchProjectFactory
import org.jenkinsci.plugins.workflow.multibranch.WorkflowMultiBranchProject
import com.cloudbees.hudson.plugins.folder.computed.DefaultOrphanedItemStrategy
import com.cloudbees.hudson.plugins.folder.computed.PeriodicFolderTrigger

def controller = Jenkins.get()
def owner = 'joeyenfield'
def repository = 'ai-control-mesh'
def remote = "https://github.com/${owner}/${repository}.git"
// During rollout, select the branch carrying these definitions. After merge, use master.
def operationsBranch = args ? args[0] : 'master'
def credentialId = 'github-ci'
def node = controller.getNode('lan-builder')
assert node != null : 'Configure the lan-builder agent first'
def environment = node.nodeProperties.get(EnvironmentVariablesNodeProperty)
assert environment?.envVars?.get('MESH_CI_HOME')?.trim() : 'Set MESH_CI_HOME on the agent first'
assert environment?.envVars?.get('MESH_DOWNLOADS_URL')?.trim() : 'Set MESH_DOWNLOADS_URL on the agent first'

// Archive old standalone jobs rather than discarding their build records.
def multibranch = { String name, String scriptPath, boolean verify ->
    def current = controller.getItem(name)
    if (current != null && !(current instanceof WorkflowMultiBranchProject)) {
        assert current instanceof WorkflowJob && !current.isBuilding()
        assert controller.getItem(name + '-legacy') == null : 'Legacy backup already exists'
        current.setDisabled(true)
        current.save()
        current.renameTo(name + '-legacy')
        current = null
    }
    def job = current ?: controller.createProject(WorkflowMultiBranchProject, name)
    def source = new GitHubSCMSource(owner, repository)
    source.setId(name + '-github')
    source.setCredentialsId(credentialId)
    def traits = [new BranchDiscoveryTrait(verify ? 1 : 3)]
    if (verify) {
        // Check PR heads, including forks, using trusted target-branch pipeline code.
        traits.add(new OriginPullRequestDiscoveryTrait(2))
        traits.add(new ForkPullRequestDiscoveryTrait(2, new ForkPullRequestDiscoveryTrait.TrustNobody()))
    }
    source.setTraits(traits)
    def branchSource = new BranchSource(source)
    if (!verify) {
        // Build master automatically; feature branches remain available for manual builds.
        def automatic = new NoTriggerBranchProperty()
        automatic.setTriggeredBranchesRegex('master')
        branchSource.setStrategy(new DefaultBranchPropertyStrategy([automatic] as BranchProperty[]))
    }
    job.getSourcesList().replaceBy([branchSource])
    def factory = new WorkflowBranchProjectFactory()
    factory.setScriptPath(scriptPath)
    job.setProjectFactory(factory)
    job.setOrphanedItemStrategy(new DefaultOrphanedItemStrategy(true, '14', '20'))
    job.addTrigger(new PeriodicFolderTrigger('5m'))
    job.setDescription(verify ? 'Branch and PR checks from Git; no deployment.' : 'Versioned distributions from Git branches. Only master builds automatically.')
    job.save()
    job.scheduleBuild2(0)
    println('Configured multibranch job: ' + name)
}

multibranch('ai-control-mesh', 'pipelines/Jenkinsfile.build', false)
multibranch('ai-control-mesh-verify', 'pipelines/Jenkinsfile.verify', true)

[
    'ai-control-mesh-test': ['pipelines/Jenkinsfile.test', [
        new StringParameterDefinition('VERSION', '', 'Exact distribution version to test.')]],
    'ai-control-mesh-distribute': ['pipelines/Jenkinsfile.distribute', [
        new ChoiceParameterDefinition('VERSION', ['Select a tested distribution'] as String[],
            'Select a deployment-tested distribution (newest first).'),
        new ChoiceParameterDefinition('TARGET', ['pi', 'github', 'both'] as String[], 'Distribution destination.')]]
].each { name, configuration ->
    def job = controller.getItem(name) ?: controller.createProject(WorkflowJob, name)
    assert job instanceof WorkflowJob && !job.isBuilding()
    def scm = new GitSCM([new UserRemoteConfig(remote, null, null, credentialId)],
        [new BranchSpec('*/' + operationsBranch)], false, [], null, null, [])
    def definition = new CpsScmFlowDefinition(scm, configuration[0])
    definition.setLightweight(true)
    job.setDefinition(definition)
    while (job.getProperty(ParametersDefinitionProperty) != null) {
        job.removeProperty(ParametersDefinitionProperty)
    }
    job.addProperty(new ParametersDefinitionProperty(configuration[1]))
    job.save()
    println('Configured SCM pipeline: ' + name)
}
controller.save()
