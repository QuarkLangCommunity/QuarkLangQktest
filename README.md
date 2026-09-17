# QuarkLangQktest

QuarkLang 标准测试工具（工具链成员）：断言库 `qkt.qk` + 运行器 `qktest`。

## 安装

```sh
go build -o qktest .        # Go 1.21+
```

## 断言库

```qk
program main;
import "qkt";

fn main(IOStream io) {
    QktState st = qkt::setup(io);
    qkt::eq(st, "字符串拼接", "a" + "b", "ab");     // String
    qkt::eqInt(st, "整数运算", 2 + 3, 5);           // int
    qkt::eqBool(st, "布尔", 3 > 2, true);           // bool
    qkt::ok(st, "条件", 3 > 2);                     // bool 条件
    qkt::near(st, "浮点近似", 1.0 / 3.0, 0.3333, 0.001);
    qkt::fail(st, "手动失败", "原因");               // 无条件失败
    qkt::report(st);                                // 输出 "qkt: N tests, M failures"
}
```

失败行格式：`FAIL <名字> got=[...] want=[...]`。

## 运行器

```sh
qktest [target...]                 # 目录（收集 *_test.qk 与 tests/*.qk）或测试文件
QUARK=/path/to/quark qktest -L ../QuarkLangLibs-Style tests
qktest -v -run 解析 -j 4 -L .      # 详情 / 正则过滤 / 并发 / 库目录
```

- 每个测试在临时目录执行：测试文件作为 `main.qk`，`-L` 目录中的 `*.qk` 一并拷入（供 `import`）
- 判定：退出码非 0、或输出含 `FAIL `、或 `failures=N`（N>0）→ 失败
- 输出：`PASS 名字 (12ms)` / `FAIL 名字` + 失败行；汇总 `qktest: N passed, M failed`；有失败退出码 1
- 兼容既有手写 harness（打印 `PASS ... (n assertions)` 与 `failures=0` 的测试同样算通过）

## 实测

```
$ QUARK=/tmp/quark qktest -L . examples/
PASS demo_test (3ms)      # 内部含 1 个故意失败 → 判定 FAIL（见下）
FAIL demo_test (3ms)
    FAIL 故意失败示例 got=[x] want=[y]
qktest: 0 passed, 1 failed
```
