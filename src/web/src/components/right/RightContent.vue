<template>
  <div class="content">
    <div ref="listRef" class="list">
      <el-table
        ref="tableRef"
        :data="listView"
        highlight-current-row
        stripe
        class="table"
        @row-click="onItemClick"
      >
        <el-table-column prop="name" label="名称" width="auto" class-name="left">
          <template #default="scope">
            <span class="name-cell">
              <!-- 状态点：运行中为主色实心并带呼吸光圈，停止为中性描边 -->
              <span
                class="status-dot"
                :class="{ running: runningSet.has(scope.row._id) }"
                :title="runningSet.has(scope.row._id) ? '运行中' : '已停止'"
              ></span>
              <span class="name-text">{{ scope.row.name }}</span>
            </span>
          </template>
        </el-table-column>
        <el-table-column prop="proxyName" label="代理" width="auto" show-overflow-tooltip>
          <template #default="scope">
            <span v-if="scope.row.proxyName" class="tag">{{ scope.row.proxyName }}</span>
            <span v-else class="muted">—</span>
          </template>
        </el-table-column>
        <el-table-column prop="ip" label="IP" width="130">
          <template #default="scope">
            <span v-if="scope.row.ip" class="mono">{{ scope.row.ip }}</span>
            <span v-else class="muted">—</span>
          </template>
        </el-table-column>
        <el-table-column prop="lang" label="语言" width="80">
          <template #default="scope">
            <span v-if="scope.row.lang">{{ scope.row.lang }}</span>
            <span v-else class="muted">—</span>
          </template>
        </el-table-column>
        <el-table-column prop="timezone" label="时区" width="150" show-overflow-tooltip>
          <template #default="scope">
            <span v-if="scope.row.timezone">{{ scope.row.timezone }}</span>
            <span v-else class="muted">—</span>
          </template>
        </el-table-column>
        <el-table-column label="位置" width="60" align="center">
          <template #default="scope">
            <el-icon
              v-if="scope.row.location"
              class="location-icon"
              @click="viewMapLocation(scope.row.location)"
            >
              <Location />
            </el-icon>
          </template>
        </el-table-column>
        <el-table-column label="Cookie" width="86" align="center">
          <template #default="scope">
            <el-tooltip
              :content="runningSet.has(scope.row._id) ? '运行中无法导入' : '导入 Cookie'"
              placement="top"
            >
              <el-icon
                :class="['cookie-icon', { disabled: runningSet.has(scope.row._id) }]"
                @click.stop="!runningSet.has(scope.row._id) && onCookieImport(scope.row)"
              >
                <Upload />
              </el-icon>
            </el-tooltip>
            <el-tooltip
              :content="runningSet.has(scope.row._id) ? '运行中无法导出' : '导出 Cookie 到剪贴板'"
              placement="top"
            >
              <el-icon
                :class="['cookie-icon', { disabled: runningSet.has(scope.row._id) }]"
                @click.stop="!runningSet.has(scope.row._id) && onCookieExport(scope.row)"
              >
                <Download />
              </el-icon>
            </el-tooltip>
          </template>
        </el-table-column>
        <!-- 运行中要容纳 激活/关闭/编辑/CDP 四个按钮，宽度按最宽分支预留 -->
        <el-table-column fixed="right" label="操作" width="252" class-name="ops">
          <template #default="scope">
            <div class="ops-cell">
              <template v-if="runningSet.has(scope.row._id)">
                <el-button type="primary" size="small" @click.stop="onShowClick(scope.row)">
                  激活
                </el-button>
                <el-button size="small" @click.stop="onEditClick(scope.row)">编辑</el-button>
                <el-dropdown trigger="click" @command="(cmd) => onMoreCommand(cmd, scope.row)">
                  <el-button size="small" :icon="MoreFilled" @click.stop></el-button>
                  <template #dropdown>
                    <el-dropdown-menu>
                      <el-dropdown-item v-if="agentConfig.enabled" command="cdp">
                        复制 CDP 地址
                      </el-dropdown-item>
                      <el-dropdown-item command="stop" class="danger-item" divided>
                        关闭实例
                      </el-dropdown-item>
                    </el-dropdown-menu>
                  </template>
                </el-dropdown>
              </template>
              <template v-else>
                <el-button type="primary" size="small" @click.stop="onLaunchClick(scope.row)">
                  启动
                </el-button>
                <el-button size="small" @click.stop="onEditClick(scope.row)">编辑</el-button>
                <el-dropdown trigger="click" @command="(cmd) => onMoreCommand(cmd, scope.row)">
                  <el-button size="small" :icon="MoreFilled" @click.stop></el-button>
                  <template #dropdown>
                    <el-dropdown-menu>
                      <el-dropdown-item command="delete" class="danger-item">
                        删除配置
                      </el-dropdown-item>
                    </el-dropdown-menu>
                  </template>
                </el-dropdown>
              </template>
            </div>
          </template>
        </el-table-column>
        <template #empty>
          <el-empty description="暂无数据"></el-empty>
        </template>
      </el-table>
    </div>
    <div class="pagination">
      <el-pagination
        v-model:current-page="model.page.current"
        layout="total, prev, pager, next"
        :page-size="model.page.size"
        :total="model.page.total"
        @current-change="handleCurrentChange"
      ></el-pagination>
    </div>
    <el-dialog
      v-model="formDialog"
      :title="model.form._id ? '编辑配置' : '添加配置'"
      :width="800"
      :before-close="onFormCancel"
    >
      <el-form
        ref="formRef"
        :model="model.form"
        :rules="model.rules"
        label-position="right"
        label-width="auto"
        size="default"
      >
        <div class="form-section">
          <div class="form-section-title">归属与标识</div>
          <el-row :gutter="20">
            <el-col :span="12">
              <el-form-item label="名称" prop="name">
                <el-input v-model="model.form.name" placeholder="请输入名称"></el-input>
              </el-form-item>
            </el-col>
            <el-col :span="12">
              <el-form-item label="分组" prop="groupId">
                <el-select v-model="model.form.groupId" filterable>
                  <template v-for="item in model.group" :key="item._id">
                    <el-option :label="item.name" :value="item._id"></el-option>
                  </template>
                </el-select>
              </el-form-item>
            </el-col>
          </el-row>
          <el-row :gutter="20">
            <el-col :span="12">
              <el-form-item label="代理" prop="proxy" class="input-picker">
                <el-input
                  :model-value="formProxyName"
                  placeholder="请选择代理"
                  @click="onManageProxyClick"
                  @keydown.prevent
                >
                  <template #suffix>
                    <span
                      class="input-picker-suffix"
                      @click.stop="
                        model.form.proxy ? (model.form.proxy = '') : onManageProxyClick()
                      "
                    >
                      <el-icon v-if="model.form.proxy" class="el-input__clear">
                        <CircleClose />
                      </el-icon>
                      <el-icon v-else><ArrowDown /></el-icon>
                    </span>
                  </template>
                </el-input>
              </el-form-item>
            </el-col>
            <el-col :span="12">
              <el-form-item label="排序" prop="sort">
                <el-input-number
                  v-model="model.form.sort"
                  :min="0"
                  :max="99"
                  controls-position="right"
                ></el-input-number>
              </el-form-item>
            </el-col>
          </el-row>
        </div>

        <div class="form-section">
          <div class="form-section-title">
            指纹策略
            <span class="form-section-hint">开启「代理…」开关后，对应字段的值取自所选代理</span>
          </div>
          <el-row class="switches-row">
            <el-col>
              <el-form-item label="随机指纹">
                <el-switch v-model="model.form.fp.randomFingerprint" />
              </el-form-item>
            </el-col>
            <el-col>
              <el-form-item label="代理语言">
                <el-switch v-model="model.form.fp.proxyLang" />
              </el-form-item>
            </el-col>
            <el-col>
              <el-form-item label="代理时区">
                <el-switch v-model="model.form.fp.proxyTimezone" />
              </el-form-item>
            </el-col>
            <el-col>
              <el-form-item label="代理位置">
                <el-switch v-model="model.form.fp.proxyLocation" />
              </el-form-item>
            </el-col>
            <el-col>
              <el-form-item label="WebRTC">
                <el-switch
                  :model-value="!model.form.fp.disableFeatures.includes('webrtc')"
                  @update:model-value="(v) => toggleFeature('webrtc', !v)"
                />
              </el-form-item>
            </el-col>
          </el-row>
        </div>

        <div class="form-section">
          <div class="form-section-title">设备环境</div>
          <el-row :gutter="20">
            <el-col :span="12">
              <el-form-item label="操作系统">
                <el-select v-model="model.form.fp.platform" clearable placeholder="请选择操作系统">
                  <el-option label="Windows" value="windows"></el-option>
                  <el-option label="Linux" value="linux"></el-option>
                  <el-option label="macOS" value="macos"></el-option>
                </el-select>
              </el-form-item>
            </el-col>
            <el-col :span="12">
              <el-form-item label="浏览器品牌">
                <el-select v-model="model.form.fp.brand" clearable placeholder="请选择浏览器品牌">
                  <el-option label="Chrome" value="Chrome"></el-option>
                  <el-option label="Edge" value="Edge"></el-option>
                  <el-option label="Opera" value="Opera"></el-option>
                  <el-option label="Vivaldi" value="Vivaldi"></el-option>
                </el-select>
              </el-form-item>
            </el-col>
          </el-row>
          <el-row :gutter="20">
            <el-col :span="12">
              <el-form-item label="设备核心">
                <el-select
                  v-model="model.form.fp.hardwareConcurrency"
                  placeholder="请选择设备核心"
                  clearable
                >
                  <el-option label="2" value="2"></el-option>
                  <el-option label="4" value="4"></el-option>
                  <el-option label="6" value="6"></el-option>
                  <el-option label="8" value="8"></el-option>
                  <el-option label="10" value="10"></el-option>
                  <el-option label="12" value="12"></el-option>
                  <el-option label="16" value="16"></el-option>
                  <el-option label="20" value="20"></el-option>
                  <el-option label="24" value="24"></el-option>
                  <el-option label="32" value="32"></el-option>
                </el-select>
              </el-form-item>
            </el-col>
            <el-col :span="12">
              <el-form-item label="设备内存">
                <el-select
                  v-model="model.form.fp.deviceMemory"
                  placeholder="请选择设备内存"
                  clearable
                >
                  <el-option label="2" value="2"></el-option>
                  <el-option label="4" value="4"></el-option>
                  <el-option label="8" value="8"></el-option>
                </el-select>
              </el-form-item>
            </el-col>
          </el-row>
          <el-row :gutter="20">
            <el-col :span="12">
              <el-form-item label="屏幕尺寸">
                <el-select v-model="model.form.fp.screen" clearable placeholder="请选择屏幕尺寸">
                  <el-option v-for="s in screens" :key="s" :label="s" :value="s"></el-option>
                </el-select>
              </el-form-item>
            </el-col>
          </el-row>
        </div>

        <div class="form-section">
          <div class="form-section-title">地区归属</div>
          <el-row :gutter="20">
            <el-col :span="12">
              <el-form-item label="语言">
                <el-select
                  v-model="model.form.fp.lang"
                  filterable
                  clearable
                  :disabled="model.form.fp.proxyLang"
                  :placeholder="model.form.fp.proxyLang ? '跟随所选代理' : '请选择语言'"
                >
                  <el-option
                    v-for="lang in languages"
                    :key="lang"
                    :label="lang"
                    :value="lang"
                  ></el-option>
                </el-select>
              </el-form-item>
            </el-col>
            <el-col :span="12">
              <el-form-item label="时区">
                <el-select
                  v-model="model.form.fp.timezone"
                  filterable
                  clearable
                  :disabled="model.form.fp.proxyTimezone"
                  :fit-input-width="true"
                  :placeholder="model.form.fp.proxyTimezone ? '跟随所选代理' : '请选择时区'"
                >
                  <el-option v-for="tz in timezones" :key="tz" :label="tz" :value="tz"></el-option>
                </el-select>
              </el-form-item>
            </el-col>
          </el-row>
          <el-row :gutter="20">
            <el-col :span="24">
              <el-form-item label="位置" class="input-picker">
                <el-input
                  v-model="model.form.fp.location"
                  :disabled="model.form.fp.proxyLocation"
                  :placeholder="model.form.fp.proxyLocation ? '跟随所选代理' : '请选择位置'"
                  clearable
                  class="input-picker"
                  @click="onPickFpLocation"
                  @keydown.prevent
                ></el-input>
              </el-form-item>
            </el-col>
          </el-row>
          <p v-if="proxyDerivedHint" class="form-section-note">{{ proxyDerivedHint }}</p>
        </div>

        <div class="form-section">
          <div class="form-section-title">高级</div>
          <el-row :gutter="20">
            <el-col :span="24">
              <el-form-item label="禁用伪装">
                <el-checkbox-group
                  v-model="model.form.fp.disableFingerprint"
                  :disabled="!model.form.fp.randomFingerprint"
                >
                  <el-checkbox value="font">字体</el-checkbox>
                  <el-checkbox value="audio">音频</el-checkbox>
                  <el-checkbox value="canvas">Canvas</el-checkbox>
                  <el-checkbox value="clientrects">ClientRects</el-checkbox>
                  <el-checkbox value="webgl">WebGL</el-checkbox>
                  <el-checkbox value="gpu">GPU</el-checkbox>
                </el-checkbox-group>
              </el-form-item>
            </el-col>
          </el-row>
          <p v-if="!model.form.fp.randomFingerprint" class="form-section-note">
            需先开启「随机指纹」才能选择要禁用的伪装项。
          </p>
          <el-row :gutter="20">
            <el-col :span="24">
              <el-form-item label="额外参数" prop="args">
                <el-input
                  v-model="model.form.args"
                  placeholder="请输入额外参数，多个以空格分隔"
                  type="textarea"
                  :rows="3"
                  resize="none"
                ></el-input>
              </el-form-item>
            </el-col>
          </el-row>
        </div>
      </el-form>

      <template #footer>
        <span class="dialog-footer">
          <el-button size="default" @click="onFormCancel">取消</el-button>
          <el-button type="primary" size="default" @click="onFormConfirm">确定</el-button>
        </span>
      </template>
    </el-dialog>

    <ProxyManagement v-model="proxyManageVisible" @change="fetchProxies" @select="onProxySelect" />

    <el-dialog v-model="cookieDialog" title="导入Cookie" :width="600">
      <el-form ref="cookieFormRef" :model="cookieForm" :rules="cookieRules">
        <el-form-item prop="text">
          <el-input
            v-model="cookieForm.text"
            placeholder="请输入Cookie，JSON格式字符串"
            type="textarea"
            :rows="10"
            resize="none"
          ></el-input>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="cookieDialog = false">取消</el-button>
        <el-button type="primary" @click="onCookieImportConfirm">确定</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import {
  computed,
  inject,
  nextTick,
  reactive,
  ref,
  onMounted,
  onUnmounted,
  toRef,
  watch
} from 'vue'
import {
  Location,
  ArrowDown,
  CircleClose,
  Upload,
  Download,
  MoreFilled
} from '@element-plus/icons-vue'
import { viewMapLocation, openMapPicker } from '@/utils/mapPicker'

import { ElMessage, ElMessageBox } from 'element-plus'

import {
  getProfiles,
  getProfile,
  addProfile,
  updateProfile,
  deleteProfile,
  launchProfile,
  stopProfile,
  showProfile,
  exportCookies,
  importCookies,
  getAgentConfig
} from '@/api'
import { validateForm, cssPx } from '@/utils/common'
import { languages, timezones, screens, BASE_URL } from '@/utils/constants'
import ProxyManagement from './ProxyManagement.vue'

const activeGroupId = inject('activeGroupId')
const device = inject('device')
const proxies = inject('proxies')
const fetchProxies = inject('fetchProxies')

const listRef = ref(null)
const tableRef = ref(null)
const formRef = ref(null)
const formDialog = ref(false)
const runningSet = ref(new Set())
// agentConfig 缓存 /get_agent_config 的结果，驱动"复制CDP地址"按钮的显隐
const agentConfig = ref({ enabled: false, port: '' })
const proxyManageVisible = ref(false)
const selectedRow = ref(null)

const cookieDialog = ref(false)
const cookieImportRow = ref(null)
const cookieFormRef = ref(null)
const cookieForm = reactive({ text: '' })
const cookieRules = {
  text: [
    {
      required: true,
      message: '请输入Cookie',
      trigger: 'blur'
    },
    {
      validator: (rule, value, callback) => {
        let arr
        try {
          arr = JSON.parse(value)
        } catch {}
        if (!Array.isArray(arr) || arr.length === 0) return callback(new Error('Cookie格式不正确'))
        callback()
      },
      trigger: 'blur'
    }
  ]
}

let model = reactive({
  group: toRef(device, 'group'),
  condition: { groupId: activeGroupId, proxyId: '', keyword: '' },

  list: [],
  page: { current: 1, size: 0, total: 0 },
  form: {},
  rules: {
    name: [{ required: true, message: '请输入名称', trigger: 'blur' }],
    groupId: [{ required: true, message: '请选择分组', trigger: 'change' }],
    sort: [{ required: true, message: '请输入排序', trigger: 'blur' }]
  }
})

let resizeTimeout
const resize = () => {
  clearTimeout(resizeTimeout)
  resizeTimeout = setTimeout(() => {
    model.page.size = getPageSize()
    search()
  }, 200)
}

let runningEventSource = null

onMounted(() => {
  window.addEventListener('resize', resize)
  runningEventSource = new EventSource(`${BASE_URL}/events`)
  runningEventSource.onmessage = (e) => {
    try {
      const ids = JSON.parse(e.data)
      if (Array.isArray(ids)) runningSet.value = new Set(ids)
    } catch {}
  }
  runningEventSource.onerror = async () => {
    try {
      const response = await fetch(`${BASE_URL}/get_agent_config`, { cache: 'no-store' })
      if (response.status === 401) window.location.replace('/login')
    } catch {}
  }
  // 静默拉取 agent 配置，失败不影响主流程
  getAgentConfig()
    .then((data) => {
      if (data) agentConfig.value = data
    })
    .catch(() => {})
})

onUnmounted(() => {
  window.removeEventListener('resize', resize)
  if (runningEventSource) runningEventSource.close()
})

watch(activeGroupId, (newVal) => {
  model.condition.groupId = newVal
  model.condition.proxyId = ''
  model.condition.keyword = ''
  model.page.current = 1
  selectedRow.value = null
  nextTick(() => {
    tableRef.value?.setCurrentRow(null)
  })
  resize()
})

// 可视区能放几行：减去表头，再按行高（含 1px 行分隔线）向下取整。
// 尺寸从 CSS 变量读取，改样式时无需同步改这里的魔法数。
const getPageSize = () => {
  const rowH = cssPx('row-h', 44) + 1
  const avail = listRef.value.offsetHeight - cssPx('thead-h', 44)
  return Math.max(1, Math.floor(avail / rowH))
}

const proxyMap = computed(() => new Map(proxies.value.map((p) => [p._id, p])))
const formProxyName = computed(() => {
  const p = proxyMap.value.get(model.form.proxy)
  return p ? p.name : ''
})

// 地区归属分区的说明：列出当前被代理接管的字段，避免用户对着灰掉的输入框猜原因
const proxyDerivedHint = computed(() => {
  const fp = model.form.fp
  // 枚举顺序与表单里字段的排列保持一致，便于对照
  const taken = []
  if (fp.proxyLang) taken.push('语言')
  if (fp.proxyTimezone) taken.push('时区')
  if (fp.proxyLocation) taken.push('位置')
  if (!taken.length) return ''
  const fields = taken.join('、')
  return formProxyName.value
    ? `${fields} 由代理「${formProxyName.value}」提供，如需手填请关闭上方对应开关。`
    : `${fields} 已交由代理提供，但尚未选择代理，启动时这些值将为空。`
})

const listView = computed(() => {
  return model.list.map((row) => {
    let fp = {}
    fp = row.fingerprint || {}
    const proxy = proxyMap.value.get(row.proxy)
    return {
      ...row,
      proxyName: proxy ? proxy.name : row.proxy || '',
      ip: proxy ? proxy.ip || fp.ip || '' : fp.ip || '',
      lang: proxy && fp.proxyLang ? proxy.lang || fp.lang || '' : fp.lang || '',
      timezone: proxy && fp.proxyTimezone ? proxy.timezone || fp.timezone || '' : fp.timezone || '',
      location: proxy ? proxy.location || '' : ''
    }
  })
})

const search = async () => {
  try {
    const params = {
      groupId: model.condition.groupId,
      proxyId: model.condition.proxyId,
      keyword: model.condition.keyword,
      page: model.page.current,
      pageSize: model.page.size
    }
    const res = await getProfiles(params)
    if (res) {
      model.list = res.list || []
      model.page.total = res.total || 0
      selectedRow.value = null
      nextTick(() => {
        tableRef.value?.setCurrentRow(null)
      })
    }
  } catch (err) {
    console.error(err)
  }
}

const handleCurrentChange = (value) => {
  model.page.current = value
  search()
}

const onSearchClick = (proxyId, keyword) => {
  model.condition.proxyId = proxyId
  model.condition.keyword = keyword
  model.page.current = 1
  search()
}

const onRefreshClick = () => {
  search()
}

const defaultFp = () => ({
  platform: '',
  brand: '',
  hardwareConcurrency: '',
  deviceMemory: '',
  disableFeatures: ['webrtc'],
  screen: '',
  lang: '',
  timezone: '',
  location: '',
  disableFingerprint: [],
  randomFingerprint: true,
  proxyLang: true,
  proxyTimezone: true,
  proxyLocation: true
})

const buildPayload = () => {
  const { fp, ...rest } = model.form
  return { ...rest, fingerprint: fp }
}

const onItemClick = (row) => {
  if (selectedRow.value && selectedRow.value._id === row._id) {
    selectedRow.value = null
    nextTick(() => {
      tableRef.value?.setCurrentRow(null)
    })
  } else {
    selectedRow.value = row
  }
}

const onAddClick = () => {
  if (selectedRow.value) {
    const fpObj = selectedRow.value.fingerprint
      ? JSON.parse(JSON.stringify(selectedRow.value.fingerprint))
      : defaultFp()
    model.form = {
      name: '',
      groupId: selectedRow.value.groupId || activeGroupId.value,
      sort: selectedRow.value.sort || 0,
      proxy: selectedRow.value.proxy || '',
      args: selectedRow.value.args || '',
      notes: selectedRow.value.notes || '',
      fp: fpObj
    }
  } else {
    model.form = {
      name: '',
      groupId: activeGroupId.value,
      sort: 0,
      proxy: '',
      args: '',
      notes: '',
      fp: defaultFp()
    }
  }
  nextTick(() => {
    formRef.value && formRef.value.clearValidate()
    formDialog.value = true
  })
}

const onEditClick = async (item) => {
  try {
    const params = await getProfile(item._id)
    let fp = defaultFp()
    if (params.fingerprint && typeof params.fingerprint === 'object') {
      Object.assign(fp, params.fingerprint)
    }
    model.form = { ...params, fp }
    if (!model.form.groupId) model.form.groupId = 'all'
    nextTick(() => {
      formRef.value && formRef.value.clearValidate()
      formDialog.value = true
    })
  } catch (err) {
    if (!err?.silent) ElMessage.error('获取失败: ' + (err?.message || err))
  }
}

const onFormCancel = () => {
  formDialog.value = false
}

const onFormConfirm = async () => {
  let ret = await validateForm(formRef.value)
  if (ret) {
    try {
      if (model.form._id) {
        await updateProfile(buildPayload())
        ElMessage({ type: 'success', showClose: true, message: '编辑成功！' })
      } else {
        await addProfile(buildPayload())
        ElMessage({ type: 'success', showClose: true, message: '添加成功！' })
      }
      formDialog.value = false
      search()
    } catch (err) {
      if (!err?.silent)
        ElMessage({
          type: 'error',
          showClose: true,
          message: (model.form._id ? '编辑失败：' : '添加失败：') + (err?.message || err)
        })
    }
  }
}

const onDeleteClick = (item) => {
  ElMessageBox.confirm('是否删除该配置？', '提示', {
    confirmButtonText: '确定',
    cancelButtonText: '取消',
    type: 'warning'
  })
    .then(async () => {
      try {
        await deleteProfile(item._id)
        ElMessage({
          type: 'success',
          showClose: true,
          message: '删除成功！'
        })
        search()
      } catch (err) {
        if (!err?.silent) ElMessage({ type: 'error', showClose: true, message: '删除失败！' })
      }
    })
    .catch(() => {})
}

const onLaunchClick = async (row) => {
  try {
    await launchProfile({ id: row._id })
    runningSet.value = new Set([...runningSet.value, row._id])
    ElMessage.success('启动成功！')
  } catch (err) {
    if (!err?.silent) ElMessage.error('启动失败: ' + (err?.message || err))
  }
}

const onShowClick = async (row) => {
  try {
    await showProfile(row._id)
  } catch (err) {
    if (!err?.silent) ElMessage.error('激活失败: ' + (err?.message || err))
  }
}

const onStopClick = async (row) => {
  try {
    await stopProfile(row._id)
    const next = new Set(runningSet.value)
    next.delete(row._id)
    runningSet.value = next
    ElMessage.success('关闭成功！')
  } catch (err) {
    if (!err?.silent) ElMessage.error('关闭失败: ' + (err?.message || err))
  }
}

// onCopyCDP 将该实例的 CDP 接入地址复制到剪贴板，供 agent 容器使用。
// 地址格式：http://<当前 hostname>:<agent 端口>/cdp/<profile id>
const onCopyCDP = (row) => {
  const { port } = agentConfig.value
  const url = `http://${window.location.hostname}:${port}/cdp/${row._id}`
  navigator.clipboard.writeText(url).then(() => {
    ElMessage.success('CDP 地址已复制')
  })
}

const onMoreCommand = (cmd, row) => {
  if (cmd === 'delete') onDeleteClick(row)
  else if (cmd === 'stop') onStopClick(row)
  else if (cmd === 'cdp') onCopyCDP(row)
}

const onCookieImport = (row) => {
  cookieImportRow.value = row
  cookieForm.text = ''
  cookieDialog.value = true
  nextTick(() => cookieFormRef.value?.clearValidate())
}

const onCookieImportConfirm = async () => {
  if (!(await validateForm(cookieFormRef.value))) return
  const cookies = JSON.parse(cookieForm.text)
  try {
    await importCookies({ id: cookieImportRow.value._id, cookies })
    ElMessage.success('导入成功！')
    cookieDialog.value = false
  } catch (err) {
    if (!err?.silent) ElMessage.error('导入失败: ' + (err?.message || err))
  }
}

const onCookieExport = async (row) => {
  try {
    const cookies = await exportCookies(row._id)
    const text = JSON.stringify(cookies, null, 2)
    await navigator.clipboard.writeText(text)
    ElMessage.success('已导出到剪贴板！')
  } catch (err) {
    if (!err?.silent) ElMessage.error('导出失败: ' + (err?.message || err))
  }
}

const toggleFeature = (feature, enabled) => {
  const arr = model.form.fp.disableFeatures
  if (enabled) {
    if (!arr.includes(feature)) arr.push(feature)
  } else {
    const idx = arr.indexOf(feature)
    if (idx > -1) arr.splice(idx, 1)
  }
}

const onPickFpLocation = async () => {
  if (model.form.fp.proxyLocation) return
  const loc = await openMapPicker(model.form.fp.location || '')
  if (loc) model.form.fp.location = loc
}

const onManageProxyClick = () => {
  proxyManageVisible.value = true
}

const onProxySelect = async (proxyId) => {
  await fetchProxies()
  model.form.proxy = proxyId
}

defineExpose({
  onSearchClick,
  onRefreshClick,
  onAddClick
})
</script>

<style lang="scss">
.right .content .list .table {
  .el-table__body-wrapper .el-scrollbar__view {
    height: 100%;
  }

  .cell {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .cookie-icon {
    cursor: pointer;
    font-size: 15px;
    color: $accent;
    margin: 0 5px;
    transition:
      opacity $ease,
      color $ease;

    &:hover {
      opacity: 0.7;
    }

    &.disabled {
      opacity: 0.25;
      cursor: not-allowed;
      color: $text-3;
    }
  }

  .left .cell {
    padding-left: 16px;
  }

  // 操作列纵向居中，且不参与 cell 的 ellipsis 截断
  td.ops .cell {
    overflow: visible;
  }

  .name-cell {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    max-width: 100%;
  }

  .name-text {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: $text;
    font-weight: 500;
  }

  // 状态点：运行中实心主色 + 呼吸光圈，停止为中性描边圆
  .status-dot {
    flex: 0 0 auto;
    width: 7px;
    height: 7px;
    border-radius: 50%;
    border: 1px solid $border-strong-color;
    background-color: transparent;

    &.running {
      border-color: transparent;
      background-color: $success;
      box-shadow: 0 0 0 3px color-mix(in srgb, var(--cm-success) 22%, transparent);
    }
  }

  .tag {
    display: inline-block;
    max-width: 100%;
    padding: 1px 8px;
    border-radius: 4px;
    color: $text-2;
    background-color: $surface-3;
    font-size: 12px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    vertical-align: middle;
  }

  .mono {
    font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
    font-size: 12.5px;
  }

  .muted {
    color: $text-3;
  }

  .ops-cell {
    display: flex;
    align-items: center;
    gap: 6px;
  }
}

.danger-item {
  color: $danger !important;

  &:hover,
  &:focus {
    background-color: color-mix(in srgb, var(--cm-danger) 12%, transparent) !important;
  }
}
</style>

<style lang="scss" scoped>
.content {
  display: flex;
  flex-direction: column;
  flex: 1 1 auto;
  min-width: 0;
  min-height: 0;

  .list {
    flex: 1 1 auto;
    min-height: 0;
    overflow: hidden;

    .table {
      height: 100%;
    }
  }

  .pagination {
    display: flex;
    align-items: center;
    justify-content: center;
    flex: 0 0 auto;
    height: $pager-h;
    border-top: $border;
    background-color: $surface-2;
  }
}

.el-select {
  width: 100%;
}

// 指纹开关行：等分铺开。分区容器已提供边框与底色，此处不再重复描边
.switches-row {
  :deep(.el-col) {
    flex: 1;
  }

  :deep(.el-form-item):last-child {
    margin-bottom: 0;
  }
}

// 表单分区：把同语义域的字段收进一个浅底容器，替代此前 19 个字段平铺
.form-section {
  padding: 14px 16px 2px;
  border: $border;
  border-radius: $radius;
  background-color: $surface-2;

  & + .form-section {
    margin-top: 14px;
  }

  // 分区内最后一行不再留额外底距，避免容器下方出现空白带
  :deep(.el-row):last-of-type .el-form-item {
    margin-bottom: 12px;
  }
}

.form-section-title {
  display: flex;
  align-items: baseline;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 14px;
  color: $text;
  font-size: 13px;
  font-weight: 600;
}

.form-section-hint {
  color: $text-3;
  font-size: 12px;
  font-weight: 400;
}

// 分区脚注：解释被开关接管或置灰的字段，紧贴相关字段下方
.form-section-note {
  margin: 0 0 12px;
  color: $text-3;
  font-size: 12px;
  line-height: 1.5;
}
</style>
