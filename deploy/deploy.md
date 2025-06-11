# Deploy mcp-registry w/ Kubernetes 

This helm chart defines resources required to run the mcp-registry project on Kubernetes.

To deploy this application, you'll need the following:

- [Install helm](https://github.com/helm/helm)
- [Install kubectl](https://kubernetes.io/docs/tasks/tools/#kubectl)

## `deploy.sh` script

This script can be used to "install" the application w/ the cluster in your current context.

> 
> Tip: To get see the cluster run `kubectl config current-context`
> 

An example of running the full command:

```bash
cd ./deploy
./deploy --namespace my-mcp-registry \
    --github-secret-name mcp-github-org \
    --registry-image example.io/mcp-registry:latest \
    --db-image example.io/mongo:latest
```

`--namespace`
This is the "Namespace" used to host all the resources created by the helm chart

`--github-secret-name`
This should be the name of a secret object deployed to the above namespace that has values for the environment variables `MCP_REGISTRY_GITHUB_CLIENT_ID` and `MCP_REGISTRY_GITHUB_CLIENT_SECRET`

There are various ways to create this secret, but I found the most convenient was to use the `--from-file` flag

>
> **Example Secrets Deployment**
> - Create a directory that will host two files, `MCP_REGISTRY_GITHUB_CLIENT_ID` and `MCP_REGISTRY_GITHUB_CLIENT_SECRET`.
>
> Your directory tree should look like this:
>
> ```
> .
> └── secrets/
>     ├── MCP_REGISTRY_GITHUB_CLIENT_ID
>     └── MCP_REGISTRY_GITHUB_CLIENT_SECRET
> ```
> 
> - Copy/Paste your github client id in `MCP_REGISTRY_GITHUB_CLIENT_ID` and client secret in `MCP_REGISTRY_GITHUB_CLIENT_SECRET`
> 
> - Run the command `kubectl create secret generic <NAME OF YOUR SECRET> --from-file=secrets\ --namespace <NAME OF YOUR NAMESPACE>`
> 
> It's important that the namespace match the namespace you plan on using with `./deploy.sh` otherwise kubernetes won't know how to inject the environment variables
>

`--registry-image`
This should be the full image reference of the mcp-registry application built from the Dockerfile at the root of this repository

`--db-image`
This should be the full image reference of the mongodb application that will serve as the data store for mcp-registry

`--env`
This value will be used to find a values "override" file, which will be applied to the template on deployment. By default the value is `test`, and the
values in [`../deploy/values.test.yaml`](../deploy/values.test.yaml) will be applied to the helm templates.

## Chart Settings

The following are important chart settings to take note of

`.Values.db.storage_class_name`
The db service deploys as a stateful set. The [storage class name](https://kubernetes.io/docs/concepts/storage/storage-classes/) should be the name
of a storage class deployed to the cluster. This facilitates mounting filesystem path for the database service to write to.

>
> Note: 
> The default value in `values.yaml` is `managed-csi`. This is a built in storage class in Azure.
> Take this into consideration when deploying to the cloud provider of your choice.
> 

`.Values.db.storage_request_size`
The requested kubernetes resource demand for disk space

`.Values.registry.port`
The internal port to use for the registry server's container

`.Values.registry.externalPort`
The public facing port of the load balancer

`.Values.registry.db`
The `db` flavor to use for the backend. Currently only the value `mongo` is supported.

### MongoDB Settings

Today `mongodb` is used as the data store for this project, but it's possible down the road this changes or additional support is added.

With that in mind, these templates allow for growing in that direction by seperating the mongodb specific settings under `.Values.db.mongo`.

Also, `db-service.yaml` and `db-statefulset.yaml` are setup so that they can swap in different configurations depending on the value of `.Values.registry.db`.

The actual definition of the mongo based resources can be found in [../deploy/templates/_helpers.tpl](../deploy/templates/_helpers.tpl)
