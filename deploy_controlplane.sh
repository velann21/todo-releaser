#!/bin/bash
set -e

echo "Deploying Control Plane Infrastructure..."

cd infrastructure-controlplane

# Ensure Pulumi stack is selected (assuming 'dev' or passed as arg)
STACK=${1:-dev}
pulumi stack select $STACK --create

echo "Running Pulumi Up..."
pulumi up -y

echo "Control Plane Deployment Complete."
