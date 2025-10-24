# SRIOV CNI

* [Meetings](#meetings)
* [How to Contribute](#contributing-code)
* [Coding Style](#coding-style)
* [Format of the patch](#format-of-the-patch)
* [Contributing Code](#contributing-code)
* [Tools](#Tools)

## Meetings
Join us for project discussios at _K8s Network & Resource management_ meetings.
The meetings take place on a weekly basis on Monday and Tuesday in alternating weeks:

* Time: 15:00 - 16:00 GMT / 10:00-11:00 ET /  07:00-08:00 PST  every other Monday
* Time: 14:00 - 15:00 GMT / 09:00-10:00 ET / 06:00-07:00 PST on every other Tuesday


* [Meeting notes and agenda](https://docs.google.com/document/d/1sJQMHbxZdeYJPgAWK1aSt6yzZ4K_8es7woVIrwinVwI/edit?usp=sharing)
* [Zoom channel](https://zoom.us/j/2392609689?pwd=K1R4ZEQyWWJVSGV3V1pmVThLMlhqQT09)

## How to Contribute

SRIOV CNI is [Apache 2.0 licensed](LICENSE) and accepts contributions via GitHub pull requests.
This document outlines some of the conventions on development workflow, commit message formatting,
contact points and other resources to make it easier to get your contribution accepted.

## Coding Style

Please follows the standard formatting recommendations and language idioms set out in [Effective Go](https://golang.org/doc/effective_go.html) and in the [Go Code Review Comments wiki](https://github.com/golang/go/wiki/CodeReviewComments).

## Format of the patch

Each patch is expected to comply with the following format:

```
Change summary

More detailed explanation of your changes: Why and how.
Wrap it to 72 characters.
See [here] (http://chris.beams.io/posts/git-commit/)
for some more good advices.

[Fixes #NUMBER (or URL to the issue)]
```

For example:

```
Fix poorly named identifiers
  
One identifier, fnname, in func.go was poorly named.  It has been renamed
to fnName.  Another identifier retval was not needed and has been removed
entirely.

Fixes #1
``` 

## Contributing Code

We always encourage the contribution for the community project. We like to collaborate with various stake holder on this project. We ask developer to keep following guidelines in mind before the contribution.

* Make sure to create an [Issue](https://github.com/k8snetworkplumbingwg/sriov-cni/issues) for bug fix or the feature request.
Issues are discussed on a regular basis at _K8s Network & Resource management_ meetings. 
* **For bugs**: For the bug fixes, please follow the issue template format while creating a issue.  If you have already found a fix, feel free to submit a Pull Request referencing the Issue you created. Include the `Fixes #` syntax to link it to the issue you're addressing.
* **For feature requests**, For the feature requests, please follow the issue template format while creating a feature requests. We want to improve upon SRIOV CNI incrementally which means small changes or features at a time.
  * Please make sure each PR are compiling or passed by Travis.
  * In order to ensure your PR can be reviewed in a timely manner, please keep PRs small 

Once you're ready to contribute code back to this repo, start with these steps:
* Fork the appropriate sub-projects that are affected by your change.
* Clone the fork to your machine:

```
$ git clone https://github.com/k8snetworkplumbingwg/sriov-cni.git
```

* Create a topic branch with prefix `dev/` for your change and checkout that branch:

```
$ git checkout -b dev/some-topic-branch
```
* Make your changes to the code and add tests to cover contributed code.
* Run `make build` to build the project.
* Run `make test` to ensure unit tests are passing and current functionality is not broken.
* Commit your changes and push them to your fork.
* Open a pull request for the appropriate project.
* Contributors will review your pull request, suggest changes, run tests and eventually merge or close the request.

> We encourage contributor to test SRIOV CNI with various NICs to check the compatibility.
> `cnitool` is a great way to test changes to sriov CNI in isolation.
> More information on cnitool can be found [here](https://github.com/containernetworking/cni/tree/master/cnitool)

## Tools
The project uses the Slack chat:
- Slack: #[Intel-SDSG-slack](https://intel-corp.herokuapp.com/) channel on slack
- Please contact contributors for issues and PR reviews.
