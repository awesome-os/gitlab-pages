<!--
# Read me first!

Create this issue under https://gitlab.com/gitlab-org/security/gitlab-pages

Set the title to: `Description of the original issue`
-->

### Prior to starting the security release work

- [ ] Read the [security process for developers] if you are not familiar with it.
- [ ] Read the [security process for components under managed versioning] if you are not familiar with it.
- [ ] Link this issue to the `gitlab-org/gitlab-pages` issue that describes the security vulnerability.

### Development

- [ ] Run `scripts/security-harness` to prevent pushing to any remote besides `security/gitlab-pages` and `dev.gitlab.org/gitlab/gitlab-pages`
- [ ] Create a new branch prefixing it with `security-`
- [ ] Create a MR targeting `master` on [`security/gitlab-pages`](https://gitlab.com/gitlab-org/security/gitlab-pages) and use the [Security Release merge request template]
- [ ] Follow the same code review process: Assign to a reviewer, then to a maintainer.

After your merge request has been approved according to our approval guidelines, and by a team member of the AppSec team, you're ready to prepare the backports

#### Backports

- [ ] Once the MR is ready to be merged, create MRs targeting the latest 3 stable branches
   * At this point, it might be easy to squash the commits from the MR into one
- [ ] Create each MR targeting the stable branch `X-Y-stable`, using the [Security Release merge request template].
   * Every merge request will have its own set of TODOs, so make sure to complete those.
- [ ] On the "Related merge requests" section, ensure all MRs are linked to this issue.
   * This section should only list the merge requests created for this issue: One targeting `master` and the 3 backports.
- [ ] If this issue requires less than 4 merge requests, post a message on the Security Release Tracking Issue and ping the Release Managers.

#### Assigning to a release

- [ ] When this issue is ready for release (Backports are approved and ready to be merged), apply the ~"security-target" label for it to be evaluated and linked to the next planned security release tracking issue.

#### Documentation and final details

- [ ] Find out the versions affected (the Git history of the files affected may help you with this) and add them to the [details section](#details)
- [ ] Fill in any upgrade notes that users may need to take into account in the [details section](#details)
- [ ] Add Yes/No and further details if needed to the migration and settings columns in the [details section](#details)
- [ ] Add the nickname of the external user who found the issue (and/or HackerOne profile) to the Thanks row in the [details section](#details)

### Summary

#### Details

| Description | Details | Further details|
| -------- | -------- | -------- |
| Versions affected | X.Y  | |
| Upgrade notes | | |
| GitLab Settings updated | Yes/No| |
| Migration required | Yes/No | |
| Thanks | | |

[security process for developers]: https://gitlab.com/gitlab-org/release/docs/blob/master/general/security/developer.md
[RM list]:  https://about.gitlab.com/release-managers/
[issue as related]: https://docs.gitlab.com/user/project/issues/related_issues.html#adding-a-related-issue
[security Release merge request template]: https://gitlab.com/gitlab-org/gitlab-pages/-/blob/master/.gitlab/merge_request_templates/Security%20Release.md
[security process for components under managed versioning]: https://gitlab.com/gitlab-org/release/docs/-/blob/master/components/managed-versioning/security_release.md

/labels ~security ~"security-notifications"
