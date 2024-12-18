#!/bin/bash

D=$(date +%s)
LABEL=deployment-$D
IMAGE=registry.fly.io/bfaas-worker:$LABEL

echo "building $IMAGE"
fly -a bfaas-worker deploy -c fly.toml.basher --update-only --image-label=$LABEL

echo "setting WORKER_IMAGE to $IMAGE"
fly -a bfaas secrets set WORKER_IMAGE=$IMAGE
