#!/bin/bash

set -e

# Required values
registry_db="mongo"
registry_image=""
registry_db_image=""
env="test"
secret_name=""
namespace="io-github-mcp"

while [[ $# -gt 0 ]]; do
    case "$1" in
        --env)
        env="$2"
        shift 2
        ;;
        --github-secret-name)
        secret_name="$2"
        shift 2
        ;;
        --registry-image)
        registry_image="$2"
        shift 2
        ;;
        --db-image)
        registry_db_image="$2"
        shift 2
        ;;
        --namespace)
        namespace="$2"
        shift 2
        ;;
        --*)
        echo "Unrecognized flag: $1"
        exit 1
        ;;
    esac
done

if [[ -z "$registry_image" ]]; then
echo "!!ERROR!!"
echo
echo "Value for --registry-image is required"
echo "Example: myregistry.io/<mcp-registry-repo>:latest"
exit 1
fi

if [[ -z "$registry_db_image" ]]; then
echo "!!ERROR!!"
echo
echo "Value for --db-image is required"
echo "Example: myregistry.io/mongo:latest"
exit 1
fi

# Check if secret_name is set and output some useful information about the expectations
if [[ -z "$secret_name" ]]; then
    echo "!!ERROR!!"
    echo
    echo "Value for --github-secret-name is required"
    echo "Expecting a secret to be deployed in the following format:"
    cat << EOF

---
apiVersion: v1
kind: Secret
metadata:
  name: <your-secret-name-here>
  namespace: $namespace
type: Opaque
data:
  MCP_REGISTRY_GITHUB_CLIENT_SECRET:
  MCP_REGISTRY_GITHUB_CLIENT_ID:


EOF
    exit 1
fi

echo "Current template for mcp-registry deployment"
helm template . \
    -f ./values.yaml \
    -f "./values.$env.yaml" \
    --set registry.db="$registry_db" \
    --set registry.image="$registry_image" \
    --set db.mongo.image="$registry_db_image" \
    --set registry.github_secret_name="$secret_name" \
    --debug

echo "Deploying App"
helm install . --generate-name --create-namespace --namespace "$namespace" \
    -f ./values.yaml \
    -f "./values.$env.yaml" \
    --set registry.db="$registry_db" \
    --set registry.image="$registry_image" \
    --set db.mongo.image="$registry_db_image" \
    --set registry.github_secret_name="$secret_name"