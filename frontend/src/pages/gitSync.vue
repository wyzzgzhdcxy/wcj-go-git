<template>
  <div class="git-sync-container">
    <el-row :gutter="0">
      <el-col :span="24">
        <el-card class="操作区">
          <template #header>
            <div class="card-header">
              <div class="header-left">
                <el-icon class="header-icon"><FolderOpened /></el-icon>
                <span class="header-title">仓库列表</span>
                <el-tag size="small" type="info">{{ repoList.length }} 个</el-tag>
              </div>
              <div class="header-right">
                <el-button type="primary" @click="selectFolder" :icon="FolderAdd">添加</el-button>
                <el-button type="success" @click="syncAll" :loading="syncing" :icon="Refresh">同步</el-button>
              </div>
            </div>
          </template>

          <el-table :data="repoList" border style="width: 100%" max-height="350">
            <el-table-column prop="name" label="仓库名称" width="150">
              <template #default="scope">
                <el-tooltip :content="scope.row.path" placement="top">
                  <span>{{ scope.row.name }}</span>
                </el-tooltip>
              </template>
            </el-table-column>
            <el-table-column prop="branch" label="分支" width="80" align="center" />
            <el-table-column label="仅提交" width="80" align="center">
              <template #default="scope">
                <el-tooltip content="仅提交，不推送" placement="top">
                  <el-switch v-model="scope.row.commitOnly" :disabled="!scope.row.enabled" @change="(val) => toggleCommitOnly(scope.row, val)" />
                </el-tooltip>
              </template>
            </el-table-column>
            <el-table-column label="自动同步" width="100" align="center">
              <template #default="scope">
                <el-switch v-model="scope.row.autoSync" :disabled="!scope.row.enabled" @change="(val) => toggleAutoSync(scope.row, val)" />
              </template>
            </el-table-column>
            <el-table-column label="启用" width="70" align="center">
              <template #default="scope">
                <el-switch v-model="scope.row.enabled" @change="saveRepos" />
              </template>
            </el-table-column>
            <el-table-column label="间隔(秒)" width="130" align="center">
              <template #default="scope">
                <el-input-number
                  v-if="scope.row.enabled && (scope.row.autoSync || scope.row.commitOnly)"
                  v-model="scope.row.intervalSeconds"
                  :min="10"
                  :max="3600"
                  :step="10"
                  size="small"
                  @change="saveRepos"
                />
                <span v-else class="text-muted">-</span>
              </template>
            </el-table-column>
            <el-table-column label="上次同步" width="100" align="center">
              <template #default="scope">
                <span v-if="scope.row.lastSyncTime" class="sync-time">
                  {{ scope.row.lastSyncTime.split('T')[1].split('Z')[0] }}
                </span>
                <span v-else class="text-muted">-</span>
              </template>
            </el-table-column>
            <el-table-column label="操作" width="80" align="center">
              <template #default="scope">
                <el-button type="danger" size="small" @click="removeRepo(scope.$index)" :icon="Delete">删除</el-button>
              </template>
            </el-table-column>
          </el-table>

          <div class="操作按钮">
          </div>
        </el-card>
      </el-col>
    </el-row>

    <el-card v-if="syncResults.length > 0" class="结果区">
      <template #header>
        <div class="header-left">
          <el-icon class="header-icon"><Document /></el-icon>
          <span class="header-title">同步结果</span>
          <el-tag size="small" type="success">{{ syncResults.filter(r => r.success).length }} 成功</el-tag>
          <el-tag size="small" type="danger" v-if="syncResults.some(r => !r.success)">{{ syncResults.filter(r => !r.success).length }} 失败</el-tag>
        </div>
      </template>
      <el-collapse>
        <el-collapse-item v-for="(result, index) in syncResults" :key="index">
          <template #title>
            <div class="result-title">
              <el-tag :type="result.success ? 'success' : 'danger'" size="small">
                {{ result.success ? '成功' : '失败' }}
              </el-tag>
              <span class="result-name">{{ result.name }}</span>
            </div>
          </template>
          <div class="log-section">
            <div class="log-block">
              <div class="log-title">Commit</div>
              <pre class="log-output">{{ result.commitLog || '无' }}</pre>
            </div>
            <div class="log-block">
              <div class="log-title">Pull</div>
              <pre class="log-output">{{ result.pullLog || '无' }}</pre>
            </div>
            <div class="log-block">
              <div class="log-title">Push</div>
              <pre class="log-output">{{ result.pushLog || '无' }}</pre>
            </div>
          </div>
        </el-collapse-item>
      </el-collapse>
    </el-card>

    <!-- 底部状态栏 -->
    <div class="status-bar">
      <span class="status-label">同步:</span>
      <span v-if="latestLogs.length === 0" class="status-empty">暂无</span>
      <template v-else>
        <span v-for="(log, index) in latestLogs" :key="log.id" class="status-item">
          <span :class="log.success ? 'status-success' : 'status-fail'">{{ log.success ? '✓' : '✗' }}</span>
          <span class="status-repo">{{ log.repoName }}</span>
          <span class="status-time">{{ formatTime(log.time) }}</span>
          <span class="status-msg">{{ log.message }}</span>
          <span v-if="index < latestLogs.length - 1" class="status-sep">|</span>
        </span>
      </template>
    </div>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { ElMessage } from 'element-plus'
import { FolderAdd, Refresh, Delete, FolderOpened, Document } from '@element-plus/icons-vue'

const repoList = ref([])
const syncing = ref(false)
const syncResults = ref([])
const syncLogs = ref([])
const autoSyncRunning = ref(false)
let refreshTimer = null

// 最新4条同步记录
const latestLogs = computed(() => syncLogs.value.slice(0, 3))

// 格式化时间
const formatTime = (timeStr) => {
  if (!timeStr) return ''
  if (timeStr.includes('T')) {
    return timeStr.split('T')[1].split('Z')[0]
  }
  return timeStr.split(' ')[1] || timeStr
}

// 加载保存的仓库列表
const loadRepos = async () => {
  try {
    const { LoadGitRepoList } = await import('../wailsjs/go/main/App.js')
    const result = await LoadGitRepoList()
    if (result.success && result.repos) {
      repoList.value = result.repos
    }
  } catch (error) {
    console.error('加载仓库列表失败:', error)
  }
}

// 加载同步日志
const loadSyncLogs = async () => {
  try {
    const { GetSyncLogs } = await import('../wailsjs/go/main/App.js')
    const result = await GetSyncLogs({ limit: 50 })
    if (result.success) {
      syncLogs.value = result.logs || []
    }
  } catch (error) {
    console.error('加载同步日志失败:', error)
  }
}

// 检查自动同步状态
const checkAutoSyncStatus = async () => {
  try {
    const { GetAutoSyncStatus } = await import('../wailsjs/go/main/App.js')
    const result = await GetAutoSyncStatus()
    autoSyncRunning.value = result.running
  } catch (error) {
    console.error('检查自动同步状态失败:', error)
  }
}

// 启动自动刷新日志和仓库列表
const startAutoRefresh = () => {
  if (refreshTimer) {
    clearInterval(refreshTimer)
  }
  refreshTimer = setInterval(() => {
    loadSyncLogs()
    loadRepos()
  }, 3000)
}

// 停止自动刷新
const stopAutoRefresh = () => {
  if (refreshTimer) {
    clearInterval(refreshTimer)
    refreshTimer = null
  }
}

// 选择文件夹
const selectFolder = async () => {
  try {
    const { SelectDirectory, GetGitRepoInfo } = await import('../wailsjs/go/main/App.js')
    const dirPath = await SelectDirectory()
    if (dirPath) {
      const result = await GetGitRepoInfo({ path: dirPath })
      if (result.success) {
        const exists = repoList.value.some(r => r.path === result.repo.path)
        if (!exists) {
          result.repo.autoSync = false
          result.repo.intervalSeconds = 60
          repoList.value.push(result.repo)
          saveRepos()
          ElMessage.success('添加成功')
        } else {
          ElMessage.warning('仓库已存在')
        }
      } else {
        ElMessage.error(result.message || '不是Git仓库')
      }
    }
  } catch (error) {
    ElMessage.error('添加失败: ' + error.message)
  }
}

// 删除仓库
const removeRepo = (index) => {
  repoList.value.splice(index, 1)
  saveRepos()
  ElMessage.success('已删除')
}

// 切换单个仓库的自动同步
const toggleAutoSync = (repo, newVal) => {
  if (newVal && repo.commitOnly) {
    repo.commitOnly = false
  }
  saveRepos()
}

// 切换单个仓库的仅提交
const toggleCommitOnly = (repo, newVal) => {
  if (newVal && repo.autoSync) {
    repo.autoSync = false
  }
  saveRepos()
}

// 保存仓库列表
const saveRepos = async () => {
  try {
    const { SaveGitRepoList } = await import('../wailsjs/go/main/App.js')
    const result = await SaveGitRepoList({ repos: repoList.value })
    if (result.success) {
      // 保存成功，静默
    } else {
      ElMessage.error(result.message)
    }
  } catch (error) {
    ElMessage.error('保存失败: ' + error.message)
  }
}

// 同步所有仓库
const syncAll = async () => {
  const enabledRepos = repoList.value.filter(r => r.enabled)
  if (enabledRepos.length === 0) {
    ElMessage.warning('没有已启用的仓库')
    return
  }

  syncing.value = true
  syncResults.value = []

  try {
    const { GitSync } = await import('../wailsjs/go/main/App.js')
    const result = await GitSync({ repos: enabledRepos })
    if (result.success) {
      syncResults.value = result.results
      ElMessage.success(result.message)
      await loadSyncLogs()
      await loadRepos()
    } else {
      ElMessage.error(result.message)
    }
  } catch (error) {
    ElMessage.error('同步失败: ' + error.message)
  } finally {
    syncing.value = false
  }
}

onMounted(() => {
  loadRepos()
  loadSyncLogs()
  checkAutoSyncStatus()
  startAutoRefresh()

  // 窗口尺寸变化时保存状态
  window.addEventListener('resize', saveWindowState)
  window.addEventListener('move', saveWindowState)
  window.addEventListener('beforeunload', saveWindowState)
})

onUnmounted(() => {
  stopAutoRefresh()
  window.removeEventListener('resize', saveWindowState)
  window.removeEventListener('move', saveWindowState)
  window.removeEventListener('beforeunload', saveWindowState)
})

// 保存窗口状态
const saveWindowState = async () => {
  try {
    const { SaveCurrentWindowState } = await import('../wailsjs/go/main/App.js')
    await SaveCurrentWindowState()
  } catch (error) {
    console.error('保存窗口状态失败:', error)
  }
}
</script>

<style scoped>
.git-sync-container {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow: hidden;
  background: #f5f7fa;
}

:deep(.el-card__body) {
  padding: 5px;
}

:deep(.el-table) {
  margin: 0;
}

:deep(.el-table__row) {
  height: 40px;
}

:deep(.el-table td) {
  padding: 5px 0;
}

.git-sync-container > .el-row {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-height: 0;
}

.git-sync-container > .el-row > .el-col {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-height: 0;
}

.git-sync-container > .el-row > .el-col > .操作区 {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-height: 0;
  height: 100%;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 0 5px;
}

.header-title {
  font-weight: 600;
  font-size: 14px;
  color: #303133;
}

.header-left {
  display: flex;
  align-items: center;
  gap: 8px;
}

.header-icon {
  font-size: 16px;
  color: #409eff;
}

.header-right {
  display: flex;
  align-items: center;
  gap: 10px;
}

:deep(.el-card__header) {
  padding: 8px 10px;
}

.操作区 {
  margin: 0 0 1px 0;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
}

:deep(.操作区 .el-card__body) {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: auto;
}

.操作按钮 {
  margin-top: 8px;
  display: flex;
  gap: 10px;
  align-items: center;
  flex-shrink: 0;
}

.status-tag {
  margin-left: 10px;
}

.result-title {
  display: flex;
  align-items: center;
  gap: 8px;
}

.result-name {
  font-weight: 500;
}

.path-text {
  margin-left: auto;
  color: #909399;
  font-size: 12px;
}

.log-output {
  background: #1e1e1e;
  color: #d4d4d4;
  padding: 10px;
  border-radius: 4px;
  max-height: 150px;
  overflow: auto;
  white-space: pre-wrap;
  word-break: break-all;
  font-size: 12px;
}

.log-section {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.log-block {
  border-left: 3px solid #409eff;
  padding-left: 10px;
}

.log-title {
  font-weight: bold;
  margin-bottom: 5px;
  font-size: 12px;
  color: #606266;
}

.结果区 {
  margin: 1px 0 0 0;
  flex-shrink: 0;
  max-height: 40%;
  overflow: auto;
}

.text-muted {
  color: #c0c4cc;
}

.sync-time {
  font-size: 12px;
  color: #606266;
}

/* 底部状态栏 */
.status-bar {
  flex-shrink: 0;
  height: 32px;
  background: #f5f7fa;
  border-top: 1px solid #e4e7ed;
  display: flex;
  align-items: center;
  padding: 0 12px;
  font-size: 12px;
}

.status-label {
  font-weight: bold;
  margin-right: 8px;
  color: #606266;
}

.status-empty {
  color: #909399;
}

.status-item {
  display: flex;
  align-items: center;
  gap: 5px;
}

.status-success {
  color: #67c23a;
  font-weight: bold;
}

.status-fail {
  color: #f56c6c;
  font-weight: bold;
}

.status-repo {
  color: #303133;
  font-weight: 500;
}

.status-time {
  color: #909399;
  font-size: 11px;
}

.status-msg {
  color: #606266;
  margin-left: 6px;
}

.status-sep {
  color: #dcdfe6;
  margin: 0 10px;
}
</style>
