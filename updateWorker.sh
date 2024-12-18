#!/bin/bash

D=$(date +%s)
LABEL=deployment-$D
IMAGE=registry.fly.io/bfaas-worker:$LABEL

echo "building $IMAGE"
fly deploy -c fly.toml.worker --update-only --image-label=$LABEL

echo "setting WORKER_IMAGE to $IMAGE"
fly secrets set WORKER_IMAGE=$IMAGE
