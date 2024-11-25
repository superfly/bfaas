# CoordBfaas

A coordinator that runs unsafe code in a work machine with strict time limits.

# What's here

- `cmd/basher`: the server that runs untrusted code.
- `cmd/genauth`: command line that generates pub/priv key pairs for basher auth.
- `cmd/genauth`: command line for generating an auth value for basher.

These files expect to get values from the environment:

- `FLY_MACHINE_ID`: machine ID needed by basher to do auth.
- `PUBLIC`: public key needed by basher to do auth
- `PRIVATE`: private key needed by coordinator to generate auth
