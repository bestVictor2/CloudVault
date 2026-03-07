# CloudVault ES 搜索与预览转码配置

本文说明如何启用：
- Elasticsearch 全文检索（`/api/file/search` 自动走 ES）
- 预览转码（非网页友好视频自动转为 mp4）

## 1. 启用 Elasticsearch

先启动 ES（本地示例）：

```powershell
docker run -d --name es `
  -p 9200:9200 `
  -e discovery.type=single-node `
  -e xpack.security.enabled=false `
  docker.elastic.co/elasticsearch/elasticsearch:8.13.4
```

然后设置环境变量（PowerShell）：

```powershell
$env:ES_ENABLED='true'
$env:ES_ADDRESS='http://localhost:9200'
$env:ES_INDEX='cloudvault_user_files'
$env:ES_TIMEOUT='5s'
$env:ES_CONTENT_MAX_BYTES='131072'
```

如果你的 ES 开启了认证，二选一配置：

```powershell
# 方式 A: ApiKey
$env:ES_API_KEY='your_api_key'

# 方式 B: 用户名密码
$env:ES_USERNAME='elastic'
$env:ES_PASSWORD='your_password'
```

说明：
- 首次搜索会自动为当前用户做一次索引回填。
- 之后新建/重命名/移动/删除/恢复都会自动同步索引。

## 2. 启用预览转码

先确保机器已安装 `ffmpeg` 并可在命令行执行。

然后设置：

```powershell
$env:PREVIEW_TRANSCODE_ENABLED='true'
$env:PREVIEW_TRANSCODE_FFMPEG='ffmpeg'
$env:PREVIEW_TRANSCODE_TIMEOUT='5m'
$env:PREVIEW_TRANSCODE_MAX_BYTES='0'
```

说明：
- 目前会对以下格式触发转码：`.avi .mkv .mov .wmv .flv .mpeg .mpg .ts .m4v .3gp .rmvb .vob`
- 转码结果写入对象存储路径：`preview/transcoded/...`
- `.mp4/.webm` 等可直接浏览器预览格式不会重复转码。

## 3. 启动服务

```powershell
go run .
```

Worker 不需要额外改动；该版本转码是按需触发（首次预览时执行）。
