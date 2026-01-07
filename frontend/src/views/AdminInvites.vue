<template>
  <div class="page">
    <Card class="sbm-surface">
      <template #title>
        <div class="header">
          <span>邀请码管理</span>
          <div class="toolbar">
            <Button
              class="p-button-outlined"
              :icon="batchDeleteMode ? 'pi pi-times' : 'pi pi-check-square'"
              :label="batchDeleteMode ? '取消选择' : '选择'"
              @click="toggleBatchDeleteMode"
            />
            <Button
              class="p-button-danger p-button-outlined"
              icon="pi pi-trash"
              label="删除"
              :disabled="!batchDeleteMode || selectedInvites.length === 0"
              @click="onBulkDeleteClick"
            />
            <Dropdown
              v-model="expiresInDays"
              :options="expiresOptions"
              optionLabel="label"
              optionValue="value"
              class="expires-dropdown"
            />
            <Button
              icon="pi pi-plus"
              label="生成邀请码"
              @click="createInvite"
            />
          </div>
        </div>
      </template>
      <template #content>
        <Message v-if="!isAdmin" severity="warn" :closable="false"
          >仅管理员可访问</Message
        >

        <div v-else class="content">
          <div class="list-toolbar">
            <SelectButton
              v-model="usedFilter"
              :options="usedFilterOptions"
              optionLabel="label"
              optionValue="value"
              aria-label="筛选是否已使用"
            />
          </div>

          <DataTable
            class="invites-table sbm-dt-fixed"
            :value="filteredInvites"
            :loading="loading"
            responsiveLayout="scroll"
            :paginator="true"
            :rows="pageSize"
            :rowsPerPageOptions="[10, 20, 50]"
            :tableStyle="{ minWidth: '980px', tableLayout: 'fixed' }"
            dataKey="id"
            v-model:selection="selectedInvites"
            @page="onPage"
          >
            <Column
              v-if="batchDeleteMode"
              selectionMode="multiple"
              :style="{ width: '48px' }"
            />
            <Column
              field="code_hint"
              header="邀请码"
              :style="{ width: '18%' }"
            >
              <template #body="{ data: row }">
                <span class="mono sbm-ellipsis" :title="row.code_hint">{{ row.code_hint }}</span>
              </template>
            </Column>
            <Column header="使用者" :style="{ width: '18%' }">
              <template #body="{ data: row }">
                <div class="user-cell">
                  <span class="sbm-ellipsis" :title="row.usedByUsername || row.usedBy || ''">
                    {{ displayUserLabel(row.usedByUsername, row.usedBy, row.usedByDeleted) }}
                  </span>
                  <small v-if="row.usedBy" class="muted sbm-ellipsis" :title="row.usedBy">ID：{{ row.usedBy }}</small>
                </div>
              </template>
            </Column>
            <Column
              field="createdAt"
              header="生成时间"
              :style="{ width: '22%' }"
            >
              <template #body="{ data: row }">{{
                formatDateTime(row.createdAt)
              }}</template>
            </Column>
            <Column
              field="expiresAt"
              header="过期时间"
              :style="{ width: '22%' }"
            >
              <template #body="{ data: row }">
                <span v-if="row.expiresAt">{{
                  formatDateTime(row.expiresAt)
                }}</span>
                <span v-else class="muted">永不过期</span>
              </template>
            </Column>
            <Column header="状态" :style="{ width: '18%' }">
              <template #body="{ data: row }">
                <Tag v-if="row.usedAt" severity="secondary" value="已使用" />
                <Tag v-else-if="row.expired" severity="danger" value="已过期" />
                <Tag v-else severity="success" value="可使用" />
              </template>
            </Column>
            <Column field="usedAt" header="使用时间" :style="{ width: '20%' }">
              <template #body="{ data: row }">
                <span v-if="row.usedAt">{{ formatDateTime(row.usedAt) }}</span>
                <span v-else class="muted">-</span>
              </template>
            </Column>
          </DataTable>
        </div>
      </template>
    </Card>

    <Dialog
      v-model:visible="lastCodeDialogVisible"
      modal
      :draggable="false"
      :style="{ width: '720px', maxWidth: '94vw' }"
      header="最新邀请码"
    >
      <div class="last-code">
        <div class="last-code-title">最新邀请码（只显示一次，请及时保存）</div>
        <div class="last-code-row">
          <span class="last-code-value">{{ lastCode }}</span>
          <Button
            class="p-button-outlined"
            icon="pi pi-copy"
            label="复制"
            @click="copyLastCode"
          />
        </div>
        <small v-if="lastExpiresHint" class="muted">{{
          lastExpiresHint
        }}</small>
      </div>

      <template #footer>
        <Button
          class="p-button-outlined"
          severity="secondary"
          label="关闭"
          @click="lastCodeDialogVisible = false"
        />
      </template>
    </Dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import Card from "primevue/card";
import Button from "primevue/button";
import DataTable from "primevue/datatable";
import Column from "primevue/column";
import Dialog from "primevue/dialog";
import Dropdown from "primevue/dropdown";
import SelectButton from "primevue/selectbutton";
import Tag from "primevue/tag";
import Message from "primevue/message";
import { useToast } from "primevue/usetoast";
import { useConfirm } from "primevue/useconfirm";
import dayjs from "dayjs";
import { authApi } from "@/api";
import { useAuthStore } from "@/stores/auth";
import { isRequestCanceled } from "@/utils/http";

type InviteRow = {
  id: string;
  code_hint: string;
  createdBy: string;
  createdByUsername?: string;
  createdByDeleted?: boolean;
  createdAt: string;
  expiresAt?: string | null;
  usedAt?: string | null;
  usedBy?: string | null;
  usedByUsername?: string;
  usedByDeleted?: boolean;
  expired: boolean;
};

const toast = useToast();
const confirm = useConfirm();
const authStore = useAuthStore();

const isAdmin = computed(() => authStore.user?.role === "admin");

const loading = ref(false);
const invites = ref<InviteRow[]>([]);
const selectedInvites = ref<InviteRow[]>([]);
const batchDeleteMode = ref(false);
const pageSize = ref(20);
const invitesAbort = ref<AbortController | null>(null);

const onPage = (e: any) => {
  pageSize.value = e?.rows || pageSize.value;
};

type UsedFilterValue = "all" | "unused" | "used";
const usedFilter = ref<UsedFilterValue>("all");
const usedFilterOptions: Array<{ label: string; value: UsedFilterValue }> = [
  { label: "全部", value: "all" },
  { label: "未使用", value: "unused" },
  { label: "已使用", value: "used" },
];

const filteredInvites = computed(() => {
  const list = invites.value;
  if (usedFilter.value === "unused") return list.filter((x) => !x.usedAt);
  if (usedFilter.value === "used") return list.filter((x) => !!x.usedAt);
  return list;
});

const expiresInDays = ref<number>(1);
const expiresOptions = [
  { label: "1 天过期", value: 1 },
  { label: "7 天过期", value: 7 },
  { label: "30 天过期", value: 30 },
  { label: "永不过期", value: 0 },
];

const lastCode = ref("");
const lastExpiresHint = ref("");
const lastCodeDialogVisible = ref(false);

const formatDateTime = (v?: string | null) => {
  if (!v) return "-";
  const d = dayjs(v);
  return d.isValid() ? d.format("YYYY-MM-DD HH:mm:ss") : v;
};

const loadInvites = async () => {
  if (!isAdmin.value) return;
  invitesAbort.value?.abort();
  const controller = new AbortController();
  invitesAbort.value = controller;
  loading.value = true;
  try {
    const res = await authApi.adminListInvites(50, { signal: controller.signal });
    if (res.data.success && res.data.data) {
      invites.value = res.data.data as any;
      return;
    }
    toast.add({
      severity: "error",
      summary: res.data.message || "获取邀请码失败",
      life: 3000,
    });
  } catch (e: any) {
    if (isRequestCanceled(e)) return;
    toast.add({
      severity: "error",
      summary: e.response?.data?.message || "获取邀请码失败",
      life: 3000,
    });
  } finally {
    loading.value = false;
    if (invitesAbort.value === controller) invitesAbort.value = null;
  }
};

const displayUserLabel = (username?: string, userId?: string | null, deleted?: boolean) => {
  const n = String(username || "").trim();
  const id = String(userId || "").trim();
  if (n) return n;
  if (deleted && id) return "（已删除）";
  return id ? id : "-";
};

const createInvite = async () => {
  if (!isAdmin.value) return;
  loading.value = true;
  try {
    const res = await authApi.adminCreateInvite(expiresInDays.value);
    if (res.data.success && res.data.data) {
      lastCode.value = res.data.data.code;
      const exp = res.data.data.expiresAt;
      lastExpiresHint.value = exp
        ? `有效期至：${formatDateTime(exp)}`
        : "有效期：永不过期";
      lastCodeDialogVisible.value = true;
      toast.add({ severity: "success", summary: "邀请码已生成", life: 2000 });
      await loadInvites();
      return;
    }
    toast.add({
      severity: "error",
      summary: res.data.message || "生成邀请码失败",
      life: 3000,
    });
  } catch (e: any) {
    toast.add({
      severity: "error",
      summary: e.response?.data?.message || "生成邀请码失败",
      life: 3000,
    });
  } finally {
    loading.value = false;
  }
};

const deleteSelected = async (rows: InviteRow[]) => {
  if (!isAdmin.value) return;
  if (rows.length === 0) return;
  loading.value = true;
  try {
    const results = await Promise.allSettled(
      rows.map((r) => authApi.adminDeleteInvite(r.id)),
    );
    let ok = 0;
    let failed = 0;
    for (const r of results) {
      if (r.status === "fulfilled" && r.value.data?.success) ok++;
      else failed++;
    }

    if (ok > 0 && failed === 0) {
      toast.add({
        severity: "success",
        summary: `已删除 ${ok} 个邀请码`,
        life: 2000,
      });
    } else if (ok > 0 && failed > 0) {
      toast.add({
        severity: "warn",
        summary: `已删除 ${ok} 个，失败 ${failed} 个`,
        life: 3000,
      });
    } else {
      toast.add({ severity: "error", summary: "删除失败", life: 3000 });
    }

    selectedInvites.value = [];
    await loadInvites();
    batchDeleteMode.value = false;
  } finally {
    loading.value = false;
  }
};

const confirmDeleteSelected = () => {
  if (!isAdmin.value) return;
  const selected = selectedInvites.value;
  if (selected.length === 0) return;

  const deletable = selected.filter((r) => !r.usedAt || r.usedByDeleted);
  const used = selected.filter((r) => !!r.usedAt && !r.usedByDeleted);
  const usedButDeletedUser = selected.filter((r) => !!r.usedAt && r.usedByDeleted);

  if (deletable.length === 0) {
    toast.add({
      severity: "warn",
      summary: "所选邀请码均已使用，无法删除",
      life: 2500,
    });
    return;
  }

  const messageParts: string[] = [];
  messageParts.push(`确定删除选中的 ${deletable.length} 个邀请码吗？`);
  if (usedButDeletedUser.length > 0) messageParts.push(`包含 ${usedButDeletedUser.length} 个“已使用但使用者已删除”的邀请码（允许删除）`);
  if (used.length > 0) messageParts.push(`已使用 ${used.length} 个将跳过`);
  const message = messageParts.join("；");

  confirm.require({
    message,
    header: "批量删除确认",
    icon: "pi pi-exclamation-triangle",
    acceptLabel: "删除",
    rejectLabel: "取消",
    acceptClass: "p-button-danger",
    accept: () => {
      void deleteSelected(deletable);
    },
  });
};

const toggleBatchDeleteMode = () => {
  if (!isAdmin.value) return;
  batchDeleteMode.value = !batchDeleteMode.value;
  selectedInvites.value = [];
};

const onBulkDeleteClick = () => {
  if (!isAdmin.value) return;
  if (!batchDeleteMode.value) return;
  if (selectedInvites.value.length === 0) {
    toast.add({
      severity: "warn",
      summary: "请先选择要删除的邀请码",
      life: 2200,
    });
    return;
  }
  confirmDeleteSelected();
};

const copyLastCode = async () => {
  if (!lastCode.value) return;
  try {
    await navigator.clipboard.writeText(lastCode.value);
    toast.add({ severity: "success", summary: "已复制到剪贴板", life: 1600 });
  } catch {
    toast.add({
      severity: "warn",
      summary: "复制失败，请手动复制",
      life: 2500,
    });
  }
};

onMounted(() => {
  loadInvites();
});

onBeforeUnmount(() => {
  invitesAbort.value?.abort();
});
</script>

<style scoped>
.page {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  flex-wrap: wrap;
}

.toolbar {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

.expires-dropdown {
  min-width: 140px;
}

.content {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.list-toolbar {
  display: flex;
  align-items: center;
  justify-content: flex-start;
  gap: 10px;
  flex-wrap: wrap;
}

.last-code {
  border: 1px dashed color-mix(in srgb, var(--p-surface-200), transparent 15%);
  border-radius: var(--radius-md);
  padding: 12px;
  background: color-mix(in srgb, var(--p-surface-0), transparent 0%);
}

.last-code-title {
  font-weight: 900;
  color: var(--p-text-color);
  margin-bottom: 8px;
}

.last-code-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  flex-wrap: wrap;
}

.last-code-value {
  font-family: var(--font-mono);
  font-weight: 800;
  letter-spacing: 0.5px;
}

.muted {
  color: var(--p-text-muted-color);
}

.mono {
  font-family: var(--font-mono);
}

.user-cell {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.invites-table :deep(.p-datatable-table) {
  width: 100%;
  table-layout: fixed;
}

.invites-table :deep(.p-datatable-thead > tr > th),
.invites-table :deep(.p-datatable-tbody > tr > td) {
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

@media (max-width: 768px) {
  .invites-table :deep(.p-datatable-table) {
    width: max-content;
    min-width: 100%;
  }
}
</style>
