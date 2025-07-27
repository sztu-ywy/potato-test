from gtts import gTTS
import os

text = "你好呀"
tts = gTTS(text=text, lang="zh-cn")
tts.save("output.mp3")

# 用 ffmpeg 转换为 WAV
os.system("ffmpeg -i output.mp3 -acodec pcm_s16le -ar 44100 output.wav")

# 删除临时文件
os.remove("output.mp3")
print("已生成 WAV 文件: output.wav")