.PHONY: help init preview deploy destroy clean dev-init prod-init dev-deploy prod-deploy

help: ## Show this help message
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-15s %s\n", $$1, $$2}'

init: ## Initialize Pulumi stack
	pulumi stack init

preview: ## Preview infrastructure changes
	pulumi preview

deploy: ## Deploy infrastructure
	pulumi up --yes

destroy: ## Destroy infrastructure
	pulumi destroy --yes

clean: ## Clean up temporary files
	rm -rf .pulumi/
	rm -f Pulumi.*.yaml.bak

dev-init: ## Initialize development stack
	pulumi stack init dev
	pulumi stack select dev

prod-init: ## Initialize production stack
	pulumi stack init prod
	pulumi stack select prod

dev-deploy: ## Deploy to development environment
	pulumi stack select dev
	pulumi up --yes

prod-deploy: ## Deploy to production environment
	pulumi stack select prod
	pulumi up --yes

dev-destroy: ## Destroy development environment
	pulumi stack select dev
	pulumi destroy --yes

prod-destroy: ## Destroy production environment
	pulumi stack select prod
	pulumi destroy --yes