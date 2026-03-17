BIN       := dashboard-server
SRC       := ./cmd/server/
CONFIG    := ./config.yaml
PM2_NAME  := dashboard

.PHONY: build run deploy stop restart logs status clean help

build: ## Build the server binary
	go build -o $(BIN) $(SRC)

run: build ## Build and run locally (foreground, no pm2)
	./$(BIN) -config $(CONFIG)

deploy: build ## Build and (re)start via pm2
	pm2 describe $(PM2_NAME) > /dev/null 2>&1 && \
		pm2 restart $(PM2_NAME) || \
		pm2 start ecosystem.config.cjs

stop: ## Stop the pm2 process
	pm2 stop $(PM2_NAME)

restart: ## Restart pm2 process without rebuilding
	pm2 restart $(PM2_NAME)

logs: ## Tail pm2 logs
	pm2 logs $(PM2_NAME)

status: ## Show pm2 status
	pm2 status

clean: ## Remove built binary
	rm -f $(BIN)

help: ## Show this help
	@grep -E '^[a-z_-]+:.*## ' $(MAKEFILE_LIST) | \
		awk -F ':.*## ' '{printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'
