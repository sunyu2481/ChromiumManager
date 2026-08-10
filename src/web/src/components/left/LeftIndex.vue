<template>
  <div class="left">
    <div class="header">
      <div class="title"><span>分组列表</span></div>
      <div class="button">
        <el-tooltip content="添加分组" placement="bottom">
          <button type="button" class="icon-button" aria-label="添加分组" @click="onAddClick">
            <el-icon><Plus /></el-icon>
          </button>
        </el-tooltip>
        <el-tooltip content="退出登录" placement="bottom">
          <button type="button" class="icon-button" aria-label="退出登录" @click="onLogoutClick">
            <el-icon><SwitchButton /></el-icon>
          </button>
        </el-tooltip>
      </div>
    </div>
    <Content ref="contentRef" />
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { Plus, SwitchButton } from '@element-plus/icons-vue'
import { logout } from '@/api'
import Content from './LeftContent.vue'

let contentRef = ref(null)
const onAddClick = () => {
  contentRef.value.onAddClick()
}

const onLogoutClick = async () => {
  try {
    await logout()
  } finally {
    window.location.replace('/login')
  }
}
</script>

<style lang="scss" scoped>
.left {
  float: left;
  width: 280px;
  height: 100%;
  border-right: $border1;

  .header {
    height: 60px;
    line-height: 60px;
    padding: 0 20px;
    background-color: $background-color2;
    color: $white-color;
    font-size: 24px;

    .title {
      float: left;
    }

    .button {
      float: right;
      display: flex;
      align-items: center;
      gap: 14px;
      height: 60px;

      .icon-button {
        display: grid;
        place-items: center;
        width: 28px;
        height: 36px;
        padding: 0;
        border: 0;
        color: inherit;
        background: transparent;
        cursor: pointer;

        &:focus-visible {
          outline: 2px solid $white-color;
          outline-offset: 1px;
        }
      }

      .el-icon {
        cursor: pointer;
        font-size: 24px;
      }
    }
  }
}
</style>
