<template>
  <div class="right">
    <div class="header">
      <div class="title">{{ currentGroupName }}</div>
      <div class="form">
        <el-input
          v-model="model.condition.keyword"
          placeholder="搜索名称 / IP / 语言 / 时区"
          clearable
          size="default"
          class="keyword"
          :prefix-icon="Search"
          @keydown.enter="onSearchClick"
          @clear="onSearchClick"
        ></el-input>
        <!-- 代理筛选独立成一个只读选择器，不再嵌进搜索框的 prepend -->
        <el-input
          :model-value="filterProxyName"
          placeholder="全部代理"
          size="default"
          class="proxy-filter input-picker"
          @click="onManageClick"
          @keydown.prevent
        >
          <template #suffix>
            <span
              class="input-picker-suffix"
              @click.stop="model.condition.proxyId ? onProxyClear() : onManageClick()"
            >
              <el-icon v-if="model.condition.proxyId" class="el-input__clear">
                <CircleClose />
              </el-icon>
              <el-icon v-else><ArrowDown /></el-icon>
            </span>
          </template>
        </el-input>
        <el-tooltip content="刷新列表" placement="bottom">
          <el-button size="default" :icon="Refresh" @click="onRefreshClick"></el-button>
        </el-tooltip>
        <el-button type="primary" size="default" :icon="Plus" @click="onAddClick">
          新建配置
        </el-button>
      </div>
    </div>
    <Content ref="contentRef" />
    <ProxyManagement v-model="proxyDialog" @change="fetchProxies" @select="onProxySelect" />
  </div>
</template>

<script setup>
import { inject, provide, reactive, ref, computed, watch, onMounted } from 'vue'
import { Search, Refresh, Plus, ArrowDown, CircleClose } from '@element-plus/icons-vue'
import { getProxies } from '@/api'

import Content from './RightContent.vue'
import ProxyManagement from './ProxyManagement.vue'

const activeGroupId = inject('activeGroupId')
const getGroupName = inject('getGroupName')
// 标题跟随左栏选中的分组，让"当前在看哪一组"一眼可见
const currentGroupName = computed(() => getGroupName(activeGroupId.value) || '配置列表')
let contentRef = ref(null)
let proxyDialog = ref(false)
let proxies = ref([])
let model = reactive({
  condition: { proxyId: '', keyword: '' }
})

const fetchProxies = async () => {
  try {
    const res = await getProxies({ all: 1 })
    if (res) proxies.value = res
  } catch (e) {
    console.error(e)
  }
}

provide('proxies', proxies)
provide('fetchProxies', fetchProxies)

onMounted(() => {
  fetchProxies()
})

watch(activeGroupId, () => {
  model.condition = { proxyId: '', keyword: '' }
})

const filterProxyName = computed(() => {
  const p = proxies.value.find((item) => item._id === model.condition.proxyId)
  return p ? p.name : ''
})

const onManageClick = () => {
  proxyDialog.value = true
}

const onProxySelect = async (proxyId) => {
  await fetchProxies()
  model.condition.proxyId = proxyId
  onSearchClick()
}

const onProxyClear = () => {
  model.condition.proxyId = ''
  onSearchClick()
}

const onSearchClick = () => {
  contentRef.value.onSearchClick(model.condition.proxyId, model.condition.keyword)
}
const onRefreshClick = () => {
  contentRef.value.onRefreshClick()
}
const onAddClick = () => {
  contentRef.value.onAddClick()
}
</script>

<style lang="scss" scoped>
.right {
  display: flex;
  flex-direction: column;
  flex: 1 1 auto;
  min-width: 0;
  min-height: 0;
  background-color: $surface;

  .header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 16px;
    flex: 0 0 auto;
    height: $toolbar-h;
    padding: 0 16px;
    border-bottom: $border;

    .title {
      flex: 0 1 auto;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
      color: $text;
      font-size: 15px;
      font-weight: 600;
    }

    .form {
      display: flex;
      align-items: center;
      gap: 8px;
      flex: 0 1 auto;
      min-width: 0;

      .keyword {
        width: 240px;
      }

      .proxy-filter {
        width: 140px;
      }
    }
  }
}
</style>
