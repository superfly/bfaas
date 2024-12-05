# CoordBfaas

A coordinator that runs unsafe code in a work machine with strict time limits.

# TODO

No network policy is set on basher worker machines at the moment.
This means that workers have access to the org's 6pn and can talk to other fly apps.

# Design

The `basher` program is installed in its own org as the `tim-basher` app. It accepts requests,
authenticates them, and runs untrusted bash commands. Aside from auth, it is a normal run of the
mill web server.

The `coord` program is installed in another org (here in my `personal` org). It is a web server
that accepts unauthenticated requests, allocates a `tim-basher` worker from a machine pool, and
proxies requests to it, and frees the machine (stoppinig it down and returning it to the pool).
It enforces a lifetime on all proxied requests, and cancels the request and shuts it down if
the request outlives its life. When proxying requests, it adds an authentication header that
is good for a limited time on a single worker machine.

The bulk of the design and implementation is in the `machines/pool` library, which manages
a pool of worker machines. It creates machines on-demand, up to the pool size. Machines are
created with a lease and machine metadata that marks which pool owns the machine, allowing
multiple pools running on different coordinator machines to allocate and manage workers
without interference. After a machine is allocated, used, and freed, it is stopped but not
destroyed. The pool will keep re-using these machines until their leases have expired.

Pools periodically perform cleaning, which destroy worker machines after their leases have
expired, both for machines owned by the pool and machines owned by other pool instances.
When a pool coordinator is stopped, it does not destroy its workers, and when restarted, can
reclaim any workers that were created by an earlier instance of the same machine.
The pool can be destroyed instead of stopped, which causes it to destroy any worker machines it owns.
If a pool machine is stopped without performing its cleanup tasks, another worker will clean up any of its
orphaned machines after their leases have expired.


# What's here

- `cmd/coord`: the server that starts basher works and proxies requests to them with a time limit.
- `cmd/basher`: the server that runs untrusted code.
- `cmd/genauth`: command line for generating an auth value for basher.
- `cmd/genkey`: command line that generates pub/priv key pairs for basher authn.

Coord expects these values from the environment:

* `MAXREQTIME`: golang format duration string for how long to let requests live. ie. `"10s"`.
* `PRIVATE`: private key to use to generate authn when making requests to workers.
* `WORKER_APP`: the app name to use when creating worker machines. If this is `mock` then a mock pool will be used.
* `WORKER_IMAGE`: the image to use when creating worker machines (!mock).
* `FLY_TOKEN`: the token to use with the machines API when creating machines (!mock).
* `FLY_REGION`: the region to spawn worker machines in (!mock).
* `FLY_MACHINE_ID`: machine ID to use as the pool name.

Basher expects these values from the environment:

* `PUBLIC`: public key to use for authn check.
* `FLY_MACHINE_ID`: machine ID to use for authn check.

# Setup

```
# Generate authn info.
% go run cmd/genkey/main.go
PUBLIC=xxx
PRIVATE=xxx
  ... capture PUBLIC=xxx PRIVATE=xxx ...

# Make tim-basher app in its own org as our worker app,
# with flycast reachable from the "personal" org.
% fly orgs create tim-basher
% fly app create tim-basher -o tim-basher
% fly -a tim-basher ips allocate-v6 --private --org personal
% fly -a tim-basher secrets set PUBLIC=$PUBLIC
% fly deploy -c fly.toml.basher --update-only
   ... capture IMAGE=registry.fly.io/tim-basher:deployment-01JE4SH5NEC28JQ5JTTGTQM78Q

# Make tim-coord app as our coordinator
% fly app create tim-coord -o personal
% fly secrets set PRIVATE=$PRIVATE
% fly secrets set WORKER_APP=tim-basher
% fly secrets set WORKER_IMAGE=$IMAGE
% fly secrets set MAXREQTIME=10s
% fly secrets set FLY_TOKEN="$(fly -a tim-basher tokens create deploy)"
% fly deploy

# Try it out
% curl -s -D- https://tim-coord.fly.dev/run -d uptime
```

To update the basher image
```
% fly deploy -c fly.toml.basher --update-only
   ... capture IMAGE=registry.fly.io/tim-basher:deployment-01JE4XPGN5KVRBE2Z1H0FWGDPA
% fly secrets set WORKER_IMAGE=$IMAGE
```

# Test locally

Coord/basher can be tested locally with a mock pool:

* Generate a key, set `PRIVATE` and `PUBLIC` in the environment
* Set `WORKER_APP` to `mock`.
* Set `MAXREQTIME` to something like `10s`.
* Set `FLY_MACHINE_ID` to something like `m8001`.
* Run coord: `go run ./cmd/coord/main.go`
* Test with curl: `curl -s -D- http://localhost:8000/run -d uptime`

