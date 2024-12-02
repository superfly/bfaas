# CoordBfaas

A coordinator that runs unsafe code in a work machine with strict time limits.

# What's here

- `cmd/coord`: the server that starts basher works and proxies requests to them with a time limit.
- `cmd/basher`: the server that runs untrusted code.
- `cmd/genauth`: command line that generates pub/priv key pairs for basher auth.
- `cmd/genauth`: command line for generating an auth value for basher.

These files expect to get values from the environment:

- `FLY_MACHINE_ID`: machine ID needed by basher to do auth.
- `PUBLIC`: public key needed by basher to do auth
- `PRIVATE`: private key needed by coordinator to generate auth

# Setup

```
% go run cmd/genkey/main.go
PUBLIC=xxxx
PRIVATE=xxx
  ... capture PUBLIC=xxx PRIVATE=xxx ...

% fly app create tim-basher -o personal
% fly -a tim-basher secrets set PUBLIC=$PUBLIC
% fly deploy -c fly.toml.basher --build-only
   ... capture IMAGE=registry.fly.io/tim-basher:deployment-01JE4SH5NEC28JQ5JTTGTQM78Q

% fly app create tim-coord -o personal
% fly secrets set PRIVATE=$PRIVATE
% fly secrets set WORKER_APP=tim-basher
% fly secrets set WORKER_IMAGE=$IMAGE
% fly secrets set MAXREQTIME=10m
% fly secrets set FLY_TOKEN="$(fly -a tim-basher tokens create deploy)"
% fly deploy

```
