build: ## Prepare træfik deps
	go mod tidy
	go mod download

log: ## Display container logs
	docker-compose logs -f

run: ## Run træfik with go-esi
	cd ../test-server && make
	docker-compose up --remove-orphans --build