# DPU-Operator CI

Because the dpu-operator requires specific hardware, it can't use
ordinary OCP clusters for CI like most other OpenShift components do.
Here is how CI works for the dpu-operator:

  - Like almost all repos in the `https://github.com/openshift/`
    organization, dpu-operator's CI is managed by the OpenShift
    [`ci-operator`], which in turn uses the upstream [`prow`] project
    to run tests on GitHub PRs (both automatically when the PR is
    created, and in response to comments like `/test`). This is all
    configured in the usual way in the [`openshift/release`] repo:

      - The (per-branch) job definitions are in
        [`ci-operator/config/openshift/dpu-operator/`].

      - The workflows used by the dpu-operator-specific jobs are
        defined in the "step registry",
        [`ci-operator/step-registry/dpu-operator/`].

      - (Additional `prow` configuration for this repo is in
        [`core-services/prow/02_config/openshift/dpu-operator/`].)

      - Further OpenShift CI documentation can be found at
        https://docs.ci.openshift.org/.

  - The step-registry workflows send a request to a (job-specific)
    "queue manager" that runs on a private OpenShift cluster internal
    to Red Hat. The queue manager waits for appropriate hardware to
    become available in our testing lab and then uses an internal
    Jenkins server to run jobs on that hardware.

      - To avoid leaking private server information into the public
        workflow definitions, the workflows use the CI operator's
        [vault functionality] to store private information, including
        the URLs for accessing the queue manager.

      - (More information about the queue manager, the Jenkins
        configuration, and the lab hardware can be found in the NHE
        repo of the internal RH GitLab installation.)

  - Each CI job uses a pair of servers in our testing lab: one with
    the hardware to be tested, and a second "provisioning host" that
    is connected both to the primary network and also directly to the
    DPU/IPU card, so it can reinstall and manage it.

  - The Jenkins jobs then run tests defined in
    [`taskfile.yaml`](./taskfile.yaml), as described in
    [README.md](./README.md).

So for example, the [`make-e2e-test-marvell` job] runs the
[`dpu-operator-e2e-tests-marvell` workflow], which in turn pulls in
the [`dpu-operator-e2e-tests-marvell-tests` workflow], which pulls in
the appropriate secrets from the CI vault and then runs
[`dpu-operator-e2e-tests-marvell-tests-commands.sh`], which makes the
appropriate requests of the queue manager.

The queue manager will then (eventually) trigger a job on the Jenkins
server that will ssh to the provisioning host for this hardware type,
check out the appropriate code from dpu-operator (the PR branch merged
back to `main`) and then run the appropriate task from
[`taskfile.yaml`](./taskfile.yaml) for the job. (In the case of
`make-e2e-test-marvell`, that is `e2e-test`.)

That task (with the help of sub-taskfiles in
[`taskfiles/`](./taskfiles/) will use `cluster-deployment-automation`
to deploy OpenShift to the DPU and its host. (Currently this is
normally a "two-cluster deployment", where the DPU runs MicroShift,
and there is a second cluster consisting of masters running in VMs on
the provisioning host plus a single worker node on the DPU's host.
However, other configurations are possible.)

Once all of this is done, the task runs the e2e tests, and reports the
results back to Jenkins, which in turn reports them back to the queue
manager, which reports them back to the CI operator, which reports
them back to GitHub.

[`ci-operator`]: https://github.com/openshift/ci-tools/
[`prow`]: https://github.com/kubernetes-sigs/prow/
[`openshift/release`]: https://github.com/openshift/release/
[`ci-operator/config/openshift/dpu-operator/`]: https://github.com/openshift/release/tree/master/ci-operator/config/openshift/dpu-operator
[`ci-operator/step-registry/dpu-operator/`]: https://github.com/openshift/release/tree/master/ci-operator/step-registry/dpu-operator
[`core-services/prow/02_config/openshift/dpu-operator/`]: https://github.com/openshift/release/tree/master/core-services/prow/02_config/openshift/dpu-operator
[vault functionality]: https://docs.ci.openshift.org/docs/how-tos/adding-a-new-secret-to-ci/
[`make-e2e-test-marvell` job]: https://github.com/openshift/release/blob/a05d029208f42f19c89ff542c44ccd0b2b9f658d/ci-operator/config/openshift/dpu-operator/openshift-dpu-operator-main.yaml#L92-L96
[`dpu-operator-e2e-tests-marvell` workflow]: https://github.com/openshift/release/blob/master/ci-operator/step-registry/dpu-operator/e2e-tests-marvell/dpu-operator-e2e-tests-marvell-workflow.yaml
[`dpu-operator-e2e-tests-marvell-tests` workflow]: https://github.com/openshift/release/blob/master/ci-operator/step-registry/dpu-operator/e2e-tests-marvell/tests/dpu-operator-e2e-tests-marvell-tests-ref.yaml
[`dpu-operator-e2e-tests-marvell-tests-commands.sh]: https://github.com/openshift/release/blob/master/ci-operator/step-registry/dpu-operator/e2e-tests-workflow/tests/dpu-operator-e2e-tests-workflow-tests-commands.sh

