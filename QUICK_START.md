# UDP + MQTT 集成测试 - 快速启动指南

## 前置条件

确保以下服务正在运行：

1. **主应用服务器** (HTTP + UDP)
   ```bash
   # 启动主应用
   go run main.go
   ```

2. **MQTT Broker** (如Mosquitto)
   ```bash
   # 安装Mosquitto (macOS)
   brew install mosquitto
   
   # 启动Mosquitto
   mosquitto -c /usr/local/etc/mosquitto/mosquitto.conf
   ```

3. **gRPC服务** (ASR和Dialog)
   ```bash
   # 确保gRPC服务正在运行
   # 具体启动命令根据你的gRPC服务配置
   ```

## 快速测试

### 1. 检查服务状态
```bash
cd test
make check
```

### 2. 运行完整测试
```bash
# 运行所有测试
make test

# 或者运行独立客户端测试
make test-client
```

### 3. 运行特定测试
```bash
# 只测试UDP音频传输
make test-udp

# 只测试WAV转Opus转换
make test-wav

# 只测试音频包构建
make test-packet
```

## 测试流程验证

### 1. HTTP认证流程
```bash
# 手动测试HTTP认证
curl -X POST http://localhost:8080/devices/auth \
  -H "Content-Type: application/json" \
  -d '{
    "version": "2",
    "language": "zh-CN",
    "mac_address": "10:51:db:72:70:a8",
    "uuid": "3c05ed7d-eae5-41ec-aebf-c284c9ddce90"
  }'
```

### 2. MQTT消息监控
```bash
# 监控MQTT消息
make monitor-mqtt
```

### 3. UDP音频传输
```bash
# 使用netcat测试UDP连接
echo "test" | nc -u localhost 8847
```

## 常见问题

### 1. 服务连接失败
```bash
# 检查端口是否被占用
lsof -i :8080  # HTTP
lsof -i :1883  # MQTT
lsof -i :8847  # UDP
```

### 2. MQTT认证失败
```bash
# 检查MQTT配置
mosquitto_sub -h localhost -t "test" -u "test" -P "test"
```

### 3. 音频文件不存在
```bash
# 创建测试音频文件
# 可以使用任何WAV文件，重命名为test_audio.wav
cp your_audio_file.wav test/test_audio.wav
```

## 调试模式

### 1. 启用详细日志
```bash
export LOG_LEVEL=debug
make test-client
```

### 2. 监控网络流量
```bash
# 监控UDP流量
sudo tcpdump -i lo0 udp port 8847

# 监控MQTT流量
sudo tcpdump -i lo0 tcp port 1883
```

### 3. 查看测试覆盖率
```bash
make coverage
# 打开 coverage.html 查看详细报告
```

## 性能测试

### 1. 音频传输性能
```bash
make perf-test
```

### 2. 并发测试
```bash
# 运行多个测试客户端
for i in {1..5}; do
  make test-client &
done
wait
```

## 配置修改

### 1. 修改服务器地址
编辑 `test/config.json`:
```json
{
  "server": {
    "http": {
      "host": "your-server-ip",
      "port": 8080
    }
  }
}
```

### 2. 修改设备信息
```json
{
  "device": {
    "mac": "your-device-mac",
    "uuid": "your-device-uuid"
  }
}
```

### 3. 修改音频配置
```json
{
  "audio": {
    "sample_rate": 24000,
    "channels": 2,
    "frame_duration": 40
  }
}
```

## 测试报告

测试完成后，查看以下信息：

1. **HTTP认证**: 检查设备是否成功认证
2. **MQTT握手**: 检查sessionID是否正确生成
3. **UDP传输**: 检查音频包是否正确发送
4. **会话清理**: 检查资源是否正确释放

## 下一步

1. 集成真实的WAV转Opus代码
2. 添加更多错误场景测试
3. 实现压力测试
4. 添加自动化测试流程 