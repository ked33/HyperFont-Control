# Web API

固定入口：`/data/adb/modules/hyperfont_control/bin/hfctl request BASE64_JSON`。响应为 `{ok:true,data:...}` 或 `{ok:false,error:"..."}`。未定义字段和操作拒绝。最大请求长度 120000 个 Base64 字符。

| 操作 | 输入 | 结果 |
|---|---|---|
| status | 无 | 字体库、草稿、active/pending、当前启动记录与文件可见性 |
| installed_apps | 无 | 按需枚举主用户应用名称、包名、UID、system 标志 |
| exemption_evidence | package | 当前存活进程的启动豁免里程碑；不证明界面文字 |
| init | 无 | 首次读取 ROM 原生字体表 |
| import_path | path, 可选 label | 仅从 Download 导入；返回内容哈希和元数据 |
| list_downloads | 无 | 列出 Download 及其下一层文件夹里的字体（跳过隐藏文件）；返回 path、name、size、modified、imported、folder。找不到 Download 时报错 |
| upload_begin | label, size | 随机 token |
| upload_chunk | token, offset, chunk | Base64 字节，最大 48 KiB；严格检查连续偏移 |
| upload_finish | token | 解析并登记字体；未完成文件不进入字体库 |
| upload_cancel | token | 只清理本次导入 |
| cleanup_uploads | 无 | 清理所有有效 token 的未完成上传，不删除字体库 |
| validate | profile | 完整语义校验与提示 |
| save | profile | 保存草稿，不切换字体 |
| stage | profile | 生成、校验、提交 pending，需手动重启 |
| restore | 无 | 提交无覆盖配置，需手动重启 |
| cancel_pending | 无 | 撤销未生效提交 |
| delete_font | id | 拒绝删除被 active/pending/draft 引用的字体 |
| export_profile | profile | 在 Download 导出配置 JSON |
| diagnostics / export_diagnostics | 无 | 诊断对象 / 在 Download 保存诊断 JSON |
| refresh_baseline | 无 | 未处于覆盖状态时重新识别 ROM |

`boot` 和 `boot-complete` 是模块生命周期的独立 CLI 命令，不通过 Web API 提供。仅开机阶段运行，不允许用 WebUI 热挂载或自动重启。

状态分层：

- draft：已保存用户设置。
- pending：已生成并通过校验，未代表当前字体已变。
- active：最近一次挂载成功选中的版本，不等同于 Android 完成开机。
- boot.state=mounted：本次启动挂载完成。
- boot.state=guarded：上次开机未确认，同一配置已暂停。
- visibilityError：内容校验失败原因，供诊断与面板显示。
- visible=true：当前命名空间中的配置与字体文件内容哈希匹配。
- 界面视觉、每个控件的字形与粗细：仍需实际应用验收。

配置契约位于 `config/profile.schema.json`，资源存在性、字体真实轴范围、重复权重和单调映射由后端再校验。示例 JSON 的全零字体 ID 是占位值，不能直接提交。

## 2.5.0 新接口

| 操作 | 输入 | 返回/行为 |
|---|---|---|
| list_presets | 无 | Preset[]：id、profile、created、updated |
| save_preset | profile，可选 id | 新建或更新方案；不修改草稿或提交 |
| delete_preset | id | 删除方案，保留字体 |
| stage_preset | id | 使用现有 Stage 校验并提交完整方案 |
| preview_prepare | id、faceIndex | 提取独立字体面，返回 token、size |
| preview_chunk | token、offset | 返回 Base64 chunk、下一 offset、总 size；每块最多 48 KiB |
| preview_release | token | 释放临时预览文件 |
| export_backup | 无 | Download 中 ZIP 路径及字体/方案数量 |
| list_backups | 无 | Download 及下一层目录 ZIP 列表 |
| prepare_restore | path 或上传 token | 完整校验并暂存，返回还原 token、fonts、presets、snapshots |
| merge_restore | 还原 token | 合并，返回新增 fonts/presets 数量；不切换配置 |
| cancel_restore | 还原 token | 释放暂存还原 |

`upload_begin` 新增可选 kind（font/backup，缺省 font）；backup 上限 4 GiB，font 保持 256 MiB。备份上传使用现有 chunk/cancel；传完后使用 prepare_restore，不调用 upload_finish。前端串行执行桥接请求。

ZIP v1 只允许 manifest.json 与 fonts/<SHA256>.font。清单 format=hyperfont-control-backup、version=1，包含原字体元数据、方案、draft/active/pending 配置快照。还原重新解析字体元数据并校验全部引用，拒绝重复路径、额外条目、链接和超限内容。方案目录有独立版本及提交修订，Profile schemaVersion 保持 1。

2.5.2 的 preview_prepare 可额外返回同源相对 url（preview-cache/<字体ID>-<字体面>.ttf）和 cached 标记。前端优先通过现有 WebUI 文件读取通道获取二进制，失败时使用同一 token 的 preview_chunk。调用者仍须 preview_release；单独的公开缓存按限额和过期策略管理，不属于备份或安装包内容。

## 2.6.0 字体覆盖

| 操作 | 输入 | 结果 |
|---|---|---|
| font_inventory | 无 | 实际字体表中的字体族、别名、用途、语言、文件和读取错误 |
| coverage_report | profile, text, language, role（用途或具体字体族） | 临时编译报告及码点预测；不提交配置、不建立新基线 |
| glyph_probe | text（1–256 字符）, language, role（字体族） | Android 塑形后的 glyph/file/ttcIndex/weight；API 29–30 只返回 hasGlyph |
| app_font_evidence | package | 目标应用的进程、映射字体与引擎库；不能证明某段文字命中了哪个字体 |

可选 profile.coverage 包含 autoFamilies、engineCompatibility 和 rules。规则按数组顺序排列，支持 role、scripts、languages、families、font；Slot.fallbacks 为最多八个不嵌套的字体设置。Emoji/符号按专用范围过滤；图标仅指定字体族的私用区。Han 规则必须指定语言。

诊断结果 verification 是证据级别，不是成功状态。glyph_probe 在独立进程优先重建 FontManager 当前配置，失败时重建预装配置，fontMapSource 标明来源；它不能代表应用已缓存的字体映射或 Unicorn/Flutter 私有字体集合。

应用豁免：配置增加可选 `exemptPackages: string[]`，最多 128 个唯一包名；省略或空数组保持旧行为。与方案和备份一起保存，手动重启生效。`status.appExemptions.boot` 含本次开机 packages、appIds、prepared、warnings、mapBytes；准备完成不等于 Zygisk 已在目标进程生效。详见 [应用字体豁免](app-exemptions.md)。
