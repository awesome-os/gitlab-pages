# Group IP address restrictions and access-controlled Pages

GitLab can [restrict group access by IP address](https://docs.gitlab.com/user/group/access_and_permissions/#restrict-group-access-by-ip-address).
This page describes how that restriction applies to GitLab Pages sites and what
a deployment needs for it to be effective.

## How it works

For sites with access control enabled (non-public visibility), Pages checks
every request with `GET /api/v4/projects/:id/pages_access`, using the user's
access token from the session cookie or the `Authorization` header.

On that request Pages sets `X-Forwarded-For` to the client address it sees
(`r.RemoteAddr` without the port). GitLab evaluates that address against the
group allowlist and returns `404 Not Found` if it is not allowed. Pages then
serves its not-found page instead of the site content.

The `X-Forwarded-For` header was added in
[!1255](https://gitlab.com/gitlab-org/gitlab-pages/-/merge_requests/1255).
Follow-up work is tracked in
[#1059](https://gitlab.com/gitlab-org/gitlab-pages/-/issues/1059).

## Deployment prerequisites

Both of the following must hold, otherwise GitLab evaluates the wrong IP address.

### GitLab must trust `X-Forwarded-For` from Pages

GitLab only uses `X-Forwarded-For` when the request comes from a trusted proxy.
The source IP of the Pages daemon, as seen by GitLab, must be covered by the
`trusted_proxies` setting (`gitlab_rails['trusted_proxies']` in
`/etc/gitlab/gitlab.rb`, or the equivalent Helm chart setting). If
`trusted_proxies` is not set, GitLab trusts private address ranges. Setting it
replaces those defaults, so the Pages host must then be listed explicitly.

If GitLab does not trust the Pages host, it evaluates the Pages host IP instead
of the user's IP:

- If the Pages host IP is in the group allowlist, the restriction is bypassed
  for every user.
- If the Pages host IP is not in the group allowlist, every user gets `404 Not Found`.

### Pages must see the real client IP

Pages forwards the address of the connection it receives. It does not trust
`X-Forwarded-For`, `X-Real-IP`, or `Forwarded` headers sent by clients to the
`listen-http`, `listen-https`, or `listen-https-proxyv2` listeners.

When Pages is behind a load balancer, `r.RemoteAddr` is the load balancer's
address unless the real client IP is passed on:

- For TCP load balancing (HTTPS), use [PROXY protocol](../README.md#proxy-protocol-for-https)
  with the `listen-https-proxyv2` listener.
- For an HTTP reverse proxy, use the `listen-proxy` listener. On this listener
  Pages trusts the forwarding headers set by the proxy, so the proxy must
  overwrite them rather than append to client-supplied values. See
  [Listening behind a reverse proxy](../README.md#listening-behind-a-reverse-proxy).

## Limitations

Sites with public visibility never call `pages_access`, so group IP address
restrictions do not apply to them.
