<template>
  <aside class="left">
    <div class="header">
      <span class="title">分组</span>
      <el-tooltip content="添加分组" placement="bottom">
        <button type="button" class="cm-icon-btn" aria-label="添加分组" @click="onAddClick">
          <el-icon><Plus /></el-icon>
        </button>
      </el-tooltip>
    </div>
    <Content ref="contentRef" />
  </aside>
</template>

<script setup>
import { ref } from 'vue'
import { Plus } from '@element-plus/icons-vue'
import Content from './LeftContent.vue'

let contentRef = ref(null)
const onAddClick = () => {
  contentRef.value.onAddClick()
}
</script>

<style lang="scss" scoped>
.left {
  display: flex;
  flex-direction: column;
  flex: 0 0 232px;
  min-height: 0;
  border-right: $border;
  background-color: $surface;

  .header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    flex: 0 0 auto;
    height: $toolbar-h;
    padding: 0 10px 0 16px;
    border-bottom: $border;

    .title {
      color: $text-3;
      font-size: 12px;
      font-weight: 600;
      letter-spacing: 0.6px;
      text-transform: uppercase;
    }
  }
}
</style>
