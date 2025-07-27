#!/bin/bash

# 集成测试运行脚本

echo "=========================================="
echo "开始运行UDP+MQTT集成测试"
echo "=========================================="

# 设置环境变量
export GO_ENV=test

# 检查依赖服务
echo "检查依赖服务..."

# 检查HTTP服务器
if curl -s http://localhost:8080/health > /dev/null 2>&1; then
    echo "✓ HTTP服务器运行正常"
else
    echo "✗ HTTP服务器未运行，请先启动主应用"
    exit 1
fi

# 检查MQTT服务器
if nc -z localhost 1883 2>/dev/null; then
    echo "✓ MQTT服务器运行正常"
else
    echo "✗ MQTT服务器未运行，请先启动MQTT Broker"
    exit 1
fi

# 检查UDP服务器
if nc -z localhost 8847 2>/dev/null; then
    echo "✓ UDP服务器运行正常"
else
    echo "✗ UDP服务器未运行，请先启动主应用"
    exit 1
fi

echo ""
echo "=========================================="
echo "运行测试用例"
echo "=========================================="

# 运行HTTP认证测试
echo "1. 运行HTTP认证测试..."
go test -v -run TestHTTPAuth ./test/

# 运行MQTT文本传输测试
echo ""
echo "2. 运行MQTT文本传输测试..."
go test -v -run TestMQTTTextTransmission ./test/

# 运行UDP音频传输测试
echo ""
echo "3. 运行UDP音频传输测试..."
go test -v -run TestUDPAudioTransmission ./test/

# 运行完整集成测试
echo ""
echo "4. 运行完整集成测试..."
go test -v -run TestIntegration ./test/

echo ""
echo "=========================================="
echo "测试完成"
echo "=========================================="

# 检查测试结果
if [ $? -eq 0 ]; then
    echo "✓ 所有测试通过"
    exit 0
else
    echo "✗ 部分测试失败"
    exit 1
fi 