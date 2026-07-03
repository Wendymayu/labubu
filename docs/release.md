# Labubu 发布指南

从源码发布 `labubu` Python 包到 TestPyPI / PyPI 的完整流程。CI 由 [.github/workflows/release.yml](../.github/workflows/release.yml) 驱动,**打 tag 即触发发布**。

## 快速发版(配置已就绪)

一切 Secrets/Variables 都配好时,发一个新版只需三步。**正式版基于 `master`,rc 基于 `develop`**(代码从 develop 合并到 master 后再打正式 tag):

```bash
# 1a. 发正式版:先把 develop 合并进 master
git checkout master && git pull origin master
git merge develop            # 或走 PR 合并
# 解决冲突后:
make test-nocgo && make build-nocgo   # 合并后本地验证一次
git push origin master

# 1b. 发 rc:直接在 develop 上打 tag,不必动 master
git checkout develop && git pull origin develop
```

```bash
# 2. 打 tag 并推送 —— 推送即触发 CI 发布
git tag v0.1.1            # 正式版(master 上):发真 PyPI
# 或预发布:git tag v0.1.1rc1  (develop 上):只发 TestPyPI
git push origin v0.1.1
```

推送后在 https://github.com/Wendymayu/labubu/actions 看 Release workflow 跑完,然后验证安装:

```bash
pip install labubu==0.1.1 && labubu version   # 应输出 labubu 0.1.1 (...)
```

看到正确版本号即发布成功。完成。

> 版本号由 CI 用 tag 名自动写入 `pyproject.toml` / `__init__.py`,**不需要手动改文件**。
> PyPI 不允许重传同一版本号 —— 要重发就 bump 版本(打 `v0.1.2` 或下一个 rc),不要重推旧 tag。
> 正式 tag 务必打在 master 上:代码已合并、master 即发布线;rc 可在 develop 上验证。

---

## 前置条件(一次性配置)

发布前必须已在 GitHub 仓库配置好以下 **Secrets** 和 **Variables**(Settings → Secrets and variables → Actions):

| 名称 | 类型 | 用途 |
|------|------|------|
| `TEST_PYPI_API_TOKEN` | Secret | TestPyPI 上传 token(test.pypi.org 账号) |
| `PYPI_API_TOKEN` | Secret | 真 PyPI 上传 token(pypi.org 账号) |
| `TEST_PYPI_ENABLED` | Variable | 值为 `true` 时启用 rc→TestPyPI 发布 |
| `PYPI_ENABLED` | Variable | 值为 `true` 时启用 final→PyPI 发布 |

> ⚠️ `vars.*` 读的是 Repository **Variables**,不是 secrets。把 `*_ENABLED` 放进 secrets 会导致 `if` 条件永远为 false、发布 job 被跳过。
> ⚠️ PyPI token 只在创建时显示一次,丢失只能重建。两个 token 是两套独立账号体系。

## 版本号约定

- **预发布(rc):** `v0.1.0rc1`、`v0.1.0rc2` … → 只发 TestPyPI,用于安装验证。
- **正式版:** `v0.1.0`(无 rc 后缀)→ 发真 PyPI。

版本号同时存在于两处,CI 会在构建时自动用 tag 名覆盖,**不需要手动改**:

- [labubu-python/pyproject.toml](../labubu-python/pyproject.toml) `version = "..."`
- [labubu-python/labubu/__init__.py](../labubu-python/labubu/__init__.py) `__version__ = "..."`

## 发布流程

### 1. 本地准备

```bash
# 正式版:切到 master 并合并 develop
git checkout master && git pull origin master
git merge develop           # 或走 PR 合并,解决冲突后 push
git push origin master

# rc:直接在 develop 上即可
git checkout develop && git pull origin develop
git status                  # 确认工作区干净

# 合并/拉取后跑测试(可选但推荐)
make test-nocgo
make build-nocgo
```

### 2. 打 tag 并推送

CI 由 tag 触发,**tag 推到 GitHub 即开始发布**:

```bash
# 预发布(rc):在 develop 上打,发 TestPyPI 验证
git tag v0.1.0rc1
git push origin v0.1.0rc1

# 验证通过后,正式版:合并到 master 后在 master 上打,发真 PyPI
git checkout master
git tag v0.1.0
git push origin v0.1.0
```

> tag 必须打在**包含最新 release.yml 修复的 commit 上** —— 标签触发的 run 用的是标签 commit 处的 workflow 文件,不是默认分支最新版。改了 workflow 后必须把 tag 移到新 commit 或重新打 tag。

### 3. 监控 CI

在 https://github.com/Wendymayu/labubu/actions 看 Release workflow:

- **build** ×5(linux amd64/arm64、windows amd64、darwin amd64/arm64)—— 全绿。
- rc tag → **publish-test** job 上传 5 个 wheel 到 TestPyPI。
- 正式 tag → **publish** job 上传 5 个 wheel 到 pypi.org。
- **github-release** job 附加 wheel + 原始二进制到 GitHub Release(失败不影响 PyPI 发布)。

### 4. 验证安装

```bash
# TestPyPI
pip install -i https://test.pypi.org/simple/ labubu==0.1.0rc1
labubu version

# 真 PyPI
pip install labubu==0.1.0
labubu version
```

应输出 `labubu 0.1.0 (windows/amd64)` 之类。

## 重新发布 / 修失败

**PyPI 不允许重新上传同一版本号。** 要重发必须 bump 版本号(新打 rc 或新正式 tag)。

### rc 失败要重发

直接打下一个 rc 号,不要移动旧 tag:

```bash
git tag v0.1.0rc2   # rc1 失败了就打 rc2
git push origin v0.1.0rc2
```

### tag 已存在但要重指向

仅当 tag 还没成功发到 PyPI 时才能这么干(已发布的版本绝不重指):

```bash
git tag -d v0.1.0rc1
git tag v0.1.0rc1 <new-commit-sha>
git push origin :refs/tags/v0.1.0rc1   # 删远端
git push origin v0.1.0rc1              # 推新
```

### 重跑 CI run

- **Re-run failed jobs** —— 安全,只重跑失败的 job。
- ⚠️ **不要点 Re-run all jobs** —— 正式版已上 PyPI,重跑 `publish` 会因版本已存在而 400 报错。

## 发布后

- 在 https://pypi.org/project/labubu/ 确认版本和 5 个 wheel 已上架。
- 在 https://github.com/Wendymayu/labubu/releases 确认 GitHub Release 存在(若缺失,在那个 run 上 Re-run failed jobs)。
- 如需更新 README 徽章 / 安装说明里的版本,单独提 PR。

## 常见坑(详见 [pypi-publish-gotchas 记忆](../../../C:/Users/wendyma/.claude/projects/d--opensource-github-labubu/memory/pypi-publish-gotchas.md))

1. **PyPI 拒收 `linux_*` 平台标签** —— 必须 `manylinux_*` / `musllinux_*`。静态 Go 二进制(CGO=0)用 `manylinux_2_17_*` 正确;Alpine/musl 不覆盖。
2. **`packages:` 输入只限定上传范围,不限 `twine check`** —— check 扫描 dist/ 全部文件。发布前有 `find dist -type f ! -name '*.whl' -delete` 清掉原始二进制。
3. **标签触发的 run 用标签 commit 的 workflow** —— 改了 release.yml 后旧 tag 重跑不会生效,必须重指/重打 tag。
4. **`vars.*` 是 Variables 不是 secrets** —— `*_ENABLED` 放错位置会导致发布 job 被静默跳过。
