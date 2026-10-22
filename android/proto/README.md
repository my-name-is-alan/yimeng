# 译梦 Android Proto（玻璃 + 涂鸦）

Kotlin + Jetpack Compose + **Haze 1.5**（API 31+ 背景模糊；&lt;31 半透明回退）。

对照 `docs/cross-platform/design-spec.md` §8 与 `doodle.md` §2.1 的 Step-1 原型：只验证玻璃胶囊（Tab / 迷你播放器 / 典藏球）与涂鸦描线+节拍呼吸，不做完整 App。

## 构建（Mac）

```bash
export JAVA_HOME=$(/usr/libexec/java_home -v 17 2>/dev/null || echo /opt/homebrew/opt/openjdk@17/libexec/openjdk.jdk/Contents/Home)
export ANDROID_HOME=/opt/homebrew/share/android-commandlinetools
cd android/proto
./gradlew :app:assembleDebug
adb install -r app/build/outputs/apk/debug/app-debug.apk
adb shell am start -n art.yimeng.proto/.MainActivity
```

## 评审截图

见提交说明中的 `/workspace/ym-proto-review/`（构建机 box 路径）。仓库内 `review-shots/` 可选本地留存，默认 gitignore。
