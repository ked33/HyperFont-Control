# 构建

需要 Go 1.24+、Python 3、Node.js、JDK 17、Android SDK，以及支持 Android target 的 Clang 和 ELF LLD。原生兼容层不依赖 C++ 运行库。标准构建用 SDK platform `android-35`、build-tools `35.0.0`，生成的 DEX 最低 API 为 29；运行时按设备的实际 Android 解析器校验。

```sh
sdkmanager 'platforms;android-35' 'build-tools;35.0.0'
export ANDROID_SDK_ROOT=/absolute/path/to/android-sdk
export JAVA_HOME=/absolute/path/to/jdk-17
export HFC_CLANG=/absolute/path/to/clang++
export HFC_LLD=/absolute/path/to/ld.lld
HFC_BUILD_JOBS=1 python3 tools/build.py
```

请使用本机实际路径。`HFC_ANDROID_PLATFORM`、`HFC_ANDROID_BUILD_TOOLS` 可选择已安装的 SDK；`HFC_DIST` 可改变产物目录。默认 `dist/` 输出 ZIP、SHA-256 与构建报告。`SOURCE.txt` 记录 Git 提交。

Mac 上如果安装了算力管家，按本机规则先查询额度并通过 `perfpilot run build --` 包装构建。GitHub Actions 在 Ubuntu 上执行同一构建脚本。

构建会运行 Go 单元测试和 vet、Web 字重回归、JavaScript/脚本语法检查，编译 arm64/x86_64 核心与 Zygisk 库、Android 校验器，最后检查 ELF 接口、16 KiB 段对齐、ZIP CRC 和无字体载荷。设备诊断测试默认跳过，不能把 CI 通过视作手机开机或视觉验收。

可选 `HFC_REAL_FONTS` 指向包含 `MiSansVF.ttf`、`MiSansLatinVF.ttf`、`xhs-font_normal.ttf` 的自有测试目录，启用历史真实字体的解析和字形数据保留检查。仓库和 Actions 不下载或分发这些文件。

## 发布与在线更新

`module.prop` 的 `updateJson` 指向本仓库 Releases 的在线更新文件。推送或 PR 只生成构建产物；版本标签通过构建后发布正式版。手动 Rebuild release assets 工作流可重新构建指定标签并更新已有 Release 附件。

只更新版本时，同时修改 `module/module.prop`、安装提示、Web 页脚、`internal/engine/model.go` 和 `tools/build.py`。发布必须使用通过检查的同一提交生成的 ZIP。

## ked33 云构建

本分支更新地址为 https://github.com/ked33/HyperFont-Control。推送 main、PR 和手动运行触发 Build module；每次构建提供包含两架构的模块 ZIP、SHA-256 和 build-report.json。打 v版本号标签会在相同校验通过后发布 Release 和在线更新元数据，普通推送不自动发布正式版。第三方 Actions 固定提交，构建任务仅有仓库读取权限，发布任务单独申请 contents:write。

本机检查发现 JDK 21、Node/Python、Android 35 SDK 和 NDK 27.2 可用，但缺少已安装 Go 与可用 Linux 执行环境。临时便携 Go 可交叉编译核心；Linux 单元测试和完整模块打包以 Actions 为准。
