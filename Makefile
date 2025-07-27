# UDP + MQTT 集成测试 Makefile

.PHONY: help test test-client test-unit clean build

# 默认目标
help:
	@echo "UDP + MQTT 集成测试"
	@echo ""
	@echo "可用命令:"
	@echo "  make test        - 运行所有测试"
	@echo "  make test-client - 运行独立测试客户端"
	@echo "  make test-unit   - 运行单元测试"
	@echo "  make build       - 构建测试客户端"
	@echo "  make clean       - 清理构建文件"
	@echo "  make check       - 检查依赖服务"

# 检查依赖服务
check:
	@echo "检查依赖服务..."
	@echo "1. 检查HTTP服务器..."
	@curl -s http://localhost:8080/health > /dev/null 2>&1 || (echo "✗ HTTP服务器未运行" && exit 1)
	@echo "✓ HTTP服务器运行正常"
	@echo "2. 检查MQTT服务器..."
	@nc -z localhost 1883 2>/dev/null || (echo "✗ MQTT服务器未运行" && exit 1)
	@echo "✓ MQTT服务器运行正常"
	@echo "3. 检查UDP服务器..."
	@nc -z localhost 8847 2>/dev/null || (echo "✗ UDP服务器未运行" && exit 1)
	@echo "✓ UDP服务器运行正常"
	@echo "所有依赖服务运行正常"

# 构建测试客户端
build:
	@echo "构建测试客户端..."
	go build -o test-client client.go

# 运行独立测试客户端
test-client: build
	@echo "运行独立测试客户端..."
	./test-client

# 运行单元测试
test-unit:
	@echo "运行单元测试..."
	go test -v ./test/

# 运行所有测试
test: check test-unit test-client

# 运行特定测试
test-integration:
	@echo "运行集成测试..."
	go test -v -run TestIntegration ./test/

test-udp:
	@echo "运行UDP音频传输测试..."
	go test -v -run TestUDPAudioTransmission ./test/

test-wav:
	@echo "运行WAV转Opus测试..."
	go test -v -run TestWavToOpusConversion ./test/

test-packet:
	@echo "运行音频包构建测试..."
	go test -v -run TestUDPAudioPacket ./test/

# 清理构建文件
clean:
	@echo "清理构建文件..."
	rm -f test-client
	go clean ./test/

# 安装依赖
deps:
	@echo "安装依赖..."
	go mod tidy
	go get github.com/eclipse/paho.mqtt.golang

# 运行测试脚本
run-script:
	@echo "运行测试脚本..."
	chmod +x run_test.sh
	./run_test.sh

# 监控MQTT消息
monitor-mqtt:
	@echo "监控MQTT消息..."
	@echo "监控公共主题: device-server"
	mosquitto_sub -h localhost -t "device-server" -v &
	@echo "监控设备主题: potato/+/td"
	mosquitto_sub -h localhost -t "potato/+/td" -v &
	@echo "监控服务端主题: potato/+/server"
	mosquitto_sub -h localhost -t "potato/+/server" -v &
	@echo "按 Ctrl+C 停止监控"
	@wait

# 性能测试
perf-test:
	@echo "运行性能测试..."
	@echo "1. 测试音频传输性能..."
	go test -v -run TestCompleteUDPAudioFlow ./test/
	@echo "2. 测试MQTT消息性能..."
	go test -v -run TestMQTTTextTransmission ./test/

# 调试模式
debug:
	@echo "启用调试模式..."
	export LOG_LEVEL=debug
	go test -v ./test/

# 显示测试覆盖率
coverage:
	@echo "生成测试覆盖率报告..."
	go test -coverprofile=coverage.out ./test/
	go tool cover -html=coverage.out -o coverage.html
	@echo "覆盖率报告已生成: coverage.html"

# 显示帮助信息
usage:
	@echo "使用说明:"
	@echo "1. 确保所有依赖服务正在运行"
	@echo "2. 运行 'make check' 检查服务状态"
	@echo "3. 运行 'make test' 执行完整测试"
	@echo "4. 运行 'make test-client' 执行独立客户端测试"
	@echo ""
	@echo "测试配置:"
	@echo "  HTTP服务器: localhost:8080"
	@echo "  MQTT服务器: localhost:1883"
	@echo "  UDP服务器: localhost:8847"
	@echo "  设备MAC: 10:51:db:72:70:a8" 