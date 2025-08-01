# 连续对话测试功能

## 功能概述

新增了连续对话测试功能，可以模拟设备进行多轮对话交互，测试系统的连续对话能力。

## 测试逻辑

连续对话测试按照以下流程进行：

1. **初始化阶段**：
   - HTTP认证
   - MQTT连接和hello握手
   - 发送IoT消息
   - 建立UDP连接

2. **连续对话轮次**（可配置轮数）：
   - 发送MQTT listen start消息
   - UDP发送音频数据
   - 发送MQTT listen stop消息
   - 等待接收音频响应
   - 重复以上步骤

3. **结束阶段**：
   - 发送goodbye消息
   - 清理资源

## 配置参数

### 连续对话次数
```go
const ContinuousDialogueCount = 3  // 可以修改这个值来改变对话轮数
```

### 其他配置
- 设备数量：`ConcurrentTestCount = 2`
- 音频文件：随机选择WAV文件
- 超时时间：30秒等待音频响应
- 轮次间隔：2秒

## 使用方法

### 方法1：交互式选择
```bash
cd test
go build -o main main.go types.go opus.go
./main
# 然后选择 "2" 运行连续对话测试
```

### 方法2：直接运行连续对话测试
```bash
cd test
go build -o main main.go types.go opus.go
echo "2" | ./main
```

### 方法3：使用测试脚本
```bash
cd test
./test_continuous_dialogue.sh
```

## 测试结果

### 输出文件
- 设备配置：`statics/test_json/device_config.json`
- 测试结果：`statics/test_json/continuous_dialogue_results_YYYYMMDD_HHMMSS.json`

### 结果统计
- 总测试数
- 成功测试数
- 成功率
- 平均总耗时
- 总对话轮数
- 成功对话轮数
- 失败对话轮数
- 对话成功率

### 详细结果
每个设备的测试结果包含：
- 设备MAC地址
- 会话ID
- 开始/结束时间
- 总耗时
- 每轮对话的详细结果
- 音频包数量统计

## 时间差计算

系统会计算并记录以下时间差：
- MQTT发送listen stop消息后到UDP收到第一个音频包的时间差
- 每轮对话的各个阶段耗时

## 日志输出

测试过程中会输出详细的日志信息：
- 设备连接状态
- 每轮对话的开始和结束
- 音频包接收情况
- 错误信息和超时警告

## 注意事项

1. 确保网络连接正常
2. 确保服务器地址配置正确
3. 音频文件需要存在于指定目录
4. 测试过程中会创建临时文件和目录
5. 建议在测试环境中运行，避免影响生产环境

## 扩展功能

可以通过修改以下参数来自定义测试：
- `ContinuousDialogueCount`：对话轮数
- `ConcurrentTestCount`：并发设备数
- 超时时间：在`executeDialogueRound`函数中修改
- 音频文件：在`getRandomWavFile`函数中添加更多文件 