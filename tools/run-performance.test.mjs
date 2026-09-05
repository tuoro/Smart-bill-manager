import assert from "node:assert/strict";
import test from "node:test";
import { waitForUploadCleanup } from "./run-performance.mjs";

test("性能上传清理等待 Worker 终止，不重试或吞掉删除冲突", async () => {
  const states = [
    { status: "queued" },
    { status: "processing" },
    { status: "failed", error_code: "provider_capability_stale" },
  ];
  let reads = 0;
  await waitForUploadCleanup(
    {
      async read(path) {
        assert.equal(path, "/jobs/synthetic-job");
        return { body: states[reads++] };
      },
    },
    "synthetic-job",
  );
  assert.equal(reads, 3);
});

test("性能上传清理拒绝非预期失败和审核状态", async () => {
  for (const body of [
    { status: "failed", error_code: "internal_error" },
    { status: "failed", error_code: "provider_config_missing" },
    { status: "needs_review" },
    { status: "completed" },
  ]) {
    await assert.rejects(
      waitForUploadCleanup(
        {
          async read() {
            return { body };
          },
        },
        "synthetic-job",
      ),
      /upload_cleanup_state_invalid/,
    );
  }
});

test("性能上传清理保留查询失败", async () => {
  const failure = new Error("synthetic-request-failed");
  await assert.rejects(
    waitForUploadCleanup(
      {
        async read() {
          throw failure;
        },
      },
      "synthetic-job",
    ),
    (error) => error === failure,
  );
});
