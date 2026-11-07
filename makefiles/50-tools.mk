.PHONY: lint goimports betteralign golangci-lint

lint: betteralign golangci-lint goimports

goimports:
	@echo "Запуск goimports"
	@find . -name "*.go" ! -name "*.gen.go" ! -path "./vendor/*" -exec go tool goimports -w -local github.com/arrowwhi {} \;

betteralign:
	@echo "Запуск betteralign"
	go tool betteralign -apply ./...

golangci-lint:
	@echo "Запуск golangci-lint"
	go tool golangci-lint run --fix
