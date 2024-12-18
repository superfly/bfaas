# CoordBfaas

A coordinator that runs unsafe code in a work machine with strict time limits.

# Design

The `basher` program is installed in its own org as the `bfaas-worker` app. It accepts requests,
authenticates them, and runs untrusted bash commands. Aside from auth, it is a normal run of the
mill web server.

The `coord` program is installed in another org (here in my `personal` org). It is a web server
that accepts unauthenticated requests, allocates a `bfaas-worker` worker from a machine pool, and
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

## Untrusted metadata

There is a subtle, but relatively weak, security flaw in this design. Untrusted worker machines
have access to the `/.fly/api` which allows them to read and alter their own metadata.
An untrusted machine can tamper with the metadata that marks which worker owns the machine.
If a pool is destroyed before it can cleanup its workers and is restarted, the new instance
of the pool will not recognize that it owned the worker machine, and wont stop or destroy
it until its lease has expired. This could allow an untrusted worker to outlive its intended
lifetime up until its lease has expired. This can only happen if the pool was terminated before
stopping the untrusted worker, which should not normally happen. A similar scenario would also
happen without tampering with the metadata if the pool was destroyed without cleaning up, and
was never restarted.


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

# Make bfaas-worker app in its own org as our worker app,
# with flycast reachable from the coordinator app.
% fly orgs create bfaas
% fly app create bfaas-worker -o bfaas
% fly -a bfaas-worker ips allocate-v6 --private --org bfaas
% fly -a bfaas-worker secrets set PUBLIC=$PUBLIC
% fly deploy -c fly.toml.basher --update-only -a bfaas-worker
   ... capture IMAGE=registry.fly.io/bfaas-worker:deployment-01JF07KZF9JEC61S0AA895PW0F

# Make bfaas app as our coordinator
% fly app create bfaas -o bfaas
% fly secrets set PRIVATE=$PRIVATE
% fly secrets set WORKER_APP=bfaas-worker
% fly secrets set WORKER_IMAGE=$IMAGE
% fly secrets set MAXREQTIME=10s
% fly secrets set FLY_TOKEN="$(fly -a bfaas-worker tokens create deploy)"
% fly deploy

# Try it out
% curl -s -D- https://bfaas.fly.dev/run -d uptime
```

To update the basher image, use the `./updateWorker.sh` script, or manually:
```
% fly -a bfaas-worker deploy -c fly.toml.basher --update-only
   ... capture IMAGE=registry.fly.io/bfaas-worker:deployment-01JE4XPGN5KVRBE2Z1H0FWGDPA
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

