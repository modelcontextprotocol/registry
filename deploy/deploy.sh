#!/bin/bash

set -e
trap 'echo "Error on line $LINENO: $BASH_COMMAND"' ERR

# Required values
registry_db="mongo"
registry_image=""
registry_image_tag=""
registry_db_image=""
registry_db_image_tag=""
env="test"
secret_name=""
namespace="io-github-mcp"
dry_run=0

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
        --registry-image-tag)
        registry_image_tag="$2"
        shift 2
        ;;
        --db-image)
        registry_db_image="$2"
        shift 2
        ;;
        --db-image-tag)
        registry_db_image_tag="$2"
        shift 2
        ;;
        --namespace)
        namespace="$2"
        shift 2
        ;;
        --dry-run)
        dry_run=1
        shift 1
        ;;
        --upgrade)
        upgrade="$2"
        shift 2
        ;;
        --*)
        echo "Unrecognized flag: $1"
        exit 1
        ;;
    esac
done

echo "Using Env: $env"

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

helm_flags=(
    -f ./values.yaml
    -f "./values.$env.yaml"
    --set registry.db="$registry_db"
    --set registry.image="$registry_image"
    --set registry.tag="$registry_image_tag"
    --set db.mongo.image="$registry_db_image"
    --set db.mongo.tag="$registry_db_image_tag"
    --set registry.github_secret_name="$secret_name"
)

echo "Current template for mcp-registry deployment"
helm template . --debug "${helm_flags[@]}"

if [[ "$dry_run" == 1 ]]; then
    helm_flags+=( "--dry-run=server" )
    helm_flags+=( "--debug" )
fi

echo "Deploying App"
if [[ -z "$upgrade" ]]; then
        echo "Installing Chart"
    helm install . --generate-name --create-namespace --namespace "$namespace" "${helm_flags[@]}"
else
    if [[ -n "$upgrade" ]]; then
        echo "Upgrading deployment '$upgrade'"
        helm upgrade --namespace "$namespace" "$upgrade" . "${helm_flags[@]}"
    else
        echo "--upgrade was set without a value, this must be the name of the deployment to upgrade"
        exit 1
    fi
fi