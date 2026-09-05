# 成员与账号管理

适用范围：`v0.4.0` 及后续新架构版本（ADR-0029）。`v0.3.4` 尚不包含以下页面及恢复命令；不能直接在旧镜像执行。

## 邀请第二名成员

管理员进入「系统设置 → 成员管理」，填写邮箱、角色与理由，创建一次性邀请。代码只在首次响应显示，有效期 48 小时；先单独保存/交给受邀人，再关闭代码。不把代码放进 URL、日志、工单或公共聊天。

受邀人在登录页点击「加入工作区」，手动粘贴代码并核对邮箱、工作区与角色。新账号设置姓名与密码；已有账号必须输入原密码，不能借加入另一工作区修改原账号。加入成功后显式登录；有多个工作区时，验证密码后选择进入的工作区。

创建响应丢失时，重试只核对同一请求，不重复创建。代码无法取回：管理员先核对邀请记录，撤销后再创建。过期、已使用或已撤销邀请均不能再加入。

## 角色、停用及自助改密

- 管理员只能修改本工作区成员的角色/状态，不得重置其他人的全局密码。四角色权限仍以页面与服务端共同执行的能力列表为准。
- 角色变更或停用后，本工作区该成员的旧会话撤销。恢复启用不恢复旧 Cookie，成员须重新登录。最后一名启用的管理员不能被停用或降级。
- 所有成员可在「账号与密码」验证当前密码后设置新密码；新密码为 12～1024 字节。此操作影响同一账号的全部工作区，并退出全部旧会话。
- 编辑冲突时选择与理由保留；刷新精确成员状态并明确核对后才可再次确认，不静默覆盖他人的变更。

## 忘记密码：本地操作者恢复

恢复是部署操作者的全局身份操作，不是工作区管理员替其他租户改密的 Web 入口。先确认本人授权、精确登录邮箱和影响范围。此处以 README 的 Compose 安装为例，在对应安装目录执行；使用包含此命令且已经完成 Schema 升级的同版本镜像。

```sh
runtime_directory=/absolute/path/to/sbm-runtime
sbm_compose() {
  docker compose --project-name smart-bill-manager \
    --env-file "$runtime_directory/deployment.env" \
    --env-file infra/compose/release.env \
    -f infra/compose/compose.yaml \
    -f infra/compose/compose.release.yaml "$@"
}
sbm_compose stop app
sbm_compose run --rm --no-deps --pull never -it app /app/recover-account --confirm-all-workspaces &&
  sbm_compose up -d --no-build --no-deps --pull never app
```

交互分别隐藏输入邮箱、新密码与恢复理由；命令只显示聚合结果，不输出身份或密码。只有恢复命令成功后才按计划重新启动应用，失败须先核对原因，不把失败当作完成。不要重跑 bootstrap、删除数据库或直接改表。

自动化运维也可使用 `--input-file /run/sbm-secrets/account-recovery-input`，将临时 JSON 以只读文件挂到 `/run/secrets/sbm_account_recovery_input`。封闭 JSON 仅含 `email`、`new_password`、`reason` 三个字符串；源文件应为 owner-only、单硬链接、非符号链接、最多 8192 字节，入口会复制为应用用户可读的私有文件。不要在命令行、普通环境变量或仓库示例中填写实际值。使用后删除仅为这次恢复创建的临时输入与容器；交互方式不需要额外输入文件。

恢复后旧密码与所有工作区会话失效，账号审计记录 `local_operator/password_recovered` 及理由；已有业务记录、成员角色和各租户归属不变。后台恢复不会伪装成某租户 Owner。

## 备份恢复边界

系统备份包含成员版本、邀请历史和全局账号审计。完整恢复验证原始快照后，撤销全部会话和尚未消费/撤销的邀请，避免旧代码重新生效；已消费/已撤销记录和账号历史保持原样。恢复后成员重新登录，管理员按需重新创建邀请。详见[备份与恢复](backup-restore.md)。
