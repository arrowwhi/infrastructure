.PHONY: test test-coverage run-example obs-up obs-down obs-logs obs-status obs-restart load-test generate-api

include makefiles/50-tools.mk

test:
	go test ./... -v


generate-api:
	cd examples/http/api/openapi && oapi-codegen -config config.yaml openapi.yaml

# Запустить полный observability стек (OTel Collector + Jaeger + Prometheus + Grafana)
obs-up:
	docker compose -f examples/docker-compose.yml up -d
	@echo "================================"
	@echo "Observability stack started!"
	@echo "================================"
	@echo "Grafana:     http://localhost:3000 (admin/admin)"
	@echo "Prometheus:  http://localhost:9090"
	@echo "Jaeger UI:   http://localhost:16686"
	@echo "OTel:        localhost:4317 (gRPC)"
	@echo "================================"

# Остановить observability стек
obs-down:
	docker compose -f examples/docker-compose.yml down

# Остановить и удалить volumes
obs-clean:
	docker compose -f examples/docker-compose.yml down -v

# Проверить статус сервисов
obs-status:
	@docker compose -f examples/docker-compose.yml ps

# Перезапустить стек
obs-restart:
	docker compose -f examples/docker-compose.yml restart

# Запустить HTTP сервер (стек должен быть уже запущен)
run-server:
	cd examples/http && go run .

# obs-up + run-server: поднять observability стек и сразу запустить пример сервера
run-example: obs-up
	@echo "Waiting for the observability stack to become ready..."
	@sleep 5
	$(MAKE) run-server

# Генерация тестовой нагрузки (требует hey: go install github.com/rakyll/hey@latest)
load-test:
	@echo "Generating load on /api/users endpoint..."
	hey -z 30s -c 10 -q 5 http://localhost:8080/api/users/123 &
	@echo "Generating load on /api/orders endpoint..."
	hey -z 30s -c 5 -q 2 -m POST http://localhost:8080/api/orders &
	@echo "Load test running for 30 seconds..."
	@echo "Check metrics at:"
	@echo "  - Grafana: http://localhost:3000/d/http-server-metrics"
	@echo "  - Prometheus: http://localhost:9090"
	@echo "  - Jaeger: http://localhost:16686"

# Простые curl запросы для тестирования
test-endpoints:
	@echo "Testing endpoints..."
	@curl -s http://localhost:8080/health | jq
	@curl -s http://localhost:8080/api/users/123 | jq
	@curl -s -X POST http://localhost:8080/api/orders -H "Content-Type: application/json" -d '{"product":1}' | jq
	@curl -s http://localhost:8080/api/external | jq
	@curl -s http://localhost:8080/api/parallel | jq
	@echo "Check errors:"
	@curl -s http://localhost:8080/api/error || true


create-client-sert:
	@cd examples/local_crt && \
		openssl genrsa -out client.key.pem 2048 && \
		openssl req -new -key client.key.pem -out client.csr -subj "/C=US/ST=State/L=City/O=Organization/CN=client" && \
		openssl x509 -req -in client.csr -CA ca.pem -CAkey ca.key.pem -CAcreateserial -out client.pem -days 365 -sha256 && \
		rm client.csr
	@echo "Client certificate created and signed by CA"

create-server-sert:
	@cd examples/local_crt && \
		rm -f server.pem server.key.pem && \
		openssl genrsa -out server.key.pem 2048 && \
		openssl req -new -key server.key.pem -out server.csr -subj "/C=US/ST=State/L=City/O=Organization/CN=localhost" && \
		echo -e "subjectAltName = @alt_names\n\n[alt_names]\nDNS.1 = localhost\nIP.1 = 127.0.0.1" > server-ext.cnf && \
		openssl x509 -req -in server.csr -CA ca.pem -CAkey ca.key.pem -CAcreateserial -out server.pem -days 365 -sha256 -extfile server-ext.cnf && \
		rm server.csr server-ext.cnf
	@echo "Server certificate created and signed by CA"
