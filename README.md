# CoordBfaas

A coordinator that runs unsafe code in a work machine with strict time limits.

# TODO

No network policy is set on basher worker machines at the moment.
This means that workers have access to the org's 6pn and can talk to other fly apps.

I turned off auto-start/auto-stop because when coord was being stopped it wasnt
cleaning up the worker pool.  Figure out why not and fix this.

# What's here

- `cmd/coord`: the server that starts basher works and proxies requests to them with a time limit.
- `cmd/basher`: the server that runs untrusted code.
- `cmd/genauth`: command line for generating an auth value for basher.
- `cmd/genkey`: command line that generates pub/priv key pairs for basher authn.

Coord expects these values from the environment:

* `MAXREQTIME`: golang format duration string for how long to let requests live. ie. `"10m"`.
* `PRIVATE`: private key to use to generate authn when making requests to workers.
* `WORKER_APP`: the app name to use when creating worker machines. If this is `mock` then a mock pool will be used.
* `WORKER_IMAGE`: the image to use when creating worker machines (!mock).
* `FLY_TOKEN`: the token to use with the machines API when creating machines (!mock).
* `FLY_REGION`: the region to spawn worker machines in (!mock).

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

# Make tim-basher app as our worker app.
% fly app create tim-basher -o personal
% fly -a tim-basher secrets set PUBLIC=$PUBLIC
% fly deploy -c fly.toml.basher --update-only
   ... capture IMAGE=registry.fly.io/tim-basher:deployment-01JE4SH5NEC28JQ5JTTGTQM78Q

# Make tim-coord app as our coordinator
% fly app create tim-coord -o personal
% fly secrets set PRIVATE=$PRIVATE
% fly secrets set WORKER_APP=tim-basher
% fly secrets set WORKER_IMAGE=$IMAGE
% fly secrets set MAXREQTIME=10m
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
* Set `MAXREQTIME` to something like `10m`.
* Set `FLY_MACHINE_ID` to something like `m8001`.
* Run coord: `go run ./cmd/coord/main.go`
* Test with curl: `curl -s -D- http://localhost:8000/run -d uptime`

