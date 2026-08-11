<template>
  <teleport to="body">
    <div v-show="state.visible" class="map-picker-overlay" @click.self="onClose">
      <div class="map-picker-dialog">
        <div class="map-picker-header">
          <span class="map-picker-title">
            {{ state.readonly ? '查看位置' : '选择位置' }}
            <span v-if="state.readonly && state.location" class="map-picker-coord">
              {{ state.location }}
            </span>
          </span>
          <button class="map-picker-close" aria-label="关闭" @click="onClose">
            <el-icon><Close /></el-icon>
          </button>
        </div>
        <div class="map-picker-body">
          <div ref="mapRef" class="map-picker-map" :class="{ dark: isDark }"></div>
          <p v-if="!state.readonly" class="map-picker-tip">点击地图任意位置即选定坐标</p>
          <div v-if="!state.readonly" class="map-picker-search">
            <el-input
              v-model="searchQuery"
              placeholder="搜索地点"
              clearable
              @keyup.enter="onSearch"
            >
              <template #append>
                <el-button :icon="Search" @click="onSearch"></el-button>
              </template>
            </el-input>
          </div>
        </div>
      </div>
    </div>
  </teleport>
</template>

<script setup>
import { ref, watch, nextTick, onUnmounted } from 'vue'
import { Search, Close } from '@element-plus/icons-vue'
import { state, resolve } from '@/utils/mapPicker'
import { isDark } from '@/utils/theme'

const mapRef = ref(null)
const searchQuery = ref('')
let mapInstance = null
let mapMarker = null

const parseLocation = (loc) => {
  if (!loc) return null
  const parts = loc.split(',')
  if (parts.length === 2) {
    const lat = parseFloat(parts[0])
    const lng = parseFloat(parts[1])
    if (!isNaN(lat) && !isNaN(lng)) return { lat, lng }
  }
  return null
}

const reverseGeocode = async (lat, lng) => {
  try {
    const resp = await fetch(
      `https://nominatim.openstreetmap.org/reverse?format=json&accept-language=${navigator.language}&lat=${lat}&lon=${lng}&zoom=10`
    )
    const data = await resp.json()
    return data?.display_name || `${lat}, ${lng}`
  } catch {
    return `${lat}, ${lng}`
  }
}

const initMap = () => {
  const L = window.L
  if (!L || !mapRef.value) return

  const pos = parseLocation(state.location)
  const lat = pos ? pos.lat : 30
  const lng = pos ? pos.lng : 114
  const zoom = pos ? 10 : 3

  if (mapInstance) {
    if (mapMarker) {
      mapMarker.remove()
      mapMarker = null
    }
    mapInstance.setView([lat, lng], zoom)
    mapInstance.invalidateSize()
    if (pos) {
      mapMarker = L.marker([lat, lng]).addTo(mapInstance)
      reverseGeocode(lat, lng).then((desc) => {
        mapMarker.bindPopup(desc).openPopup()
      })
    }
    return
  }

  mapInstance = L.map(mapRef.value, { zoomControl: false }).setView([lat, lng], zoom)
  L.control.zoom({ position: 'topright' }).addTo(mapInstance)
  L.tileLayer('https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png', {
    attribution: '&copy; OpenStreetMap',
    maxZoom: 18
  }).addTo(mapInstance)

  if (pos) {
    mapMarker = L.marker([lat, lng]).addTo(mapInstance)
    reverseGeocode(lat, lng).then((desc) => {
      mapMarker.bindPopup(desc).openPopup()
    })
  }

  mapInstance.on('click', (e) => {
    if (state.readonly) return
    const { lat: newLat, lng: newLng } = e.latlng
    const loc = newLat.toFixed(3) + ',' + newLng.toFixed(3)
    if (mapMarker) {
      mapMarker.setLatLng(e.latlng)
    } else {
      mapMarker = L.marker(e.latlng).addTo(mapInstance)
    }
    reverseGeocode(newLat, newLng).then((desc) => {
      mapMarker.bindPopup(desc).openPopup()
    })
    resolve(loc)
  })
}

const destroyMap = () => {
  if (mapInstance) {
    mapInstance.remove()
    mapInstance = null
    mapMarker = null
  }
}

const onClose = () => {
  resolve(null)
}

const onSearch = async () => {
  const q = searchQuery.value.trim()
  if (!q || !mapInstance) return
  try {
    const resp = await fetch(
      `https://nominatim.openstreetmap.org/search?format=json&q=${encodeURIComponent(q)}&limit=1`
    )
    const data = await resp.json()
    if (data && data.length > 0) {
      const { lat, lon, display_name } = data[0]
      const latNum = parseFloat(lat)
      const lngNum = parseFloat(lon)
      mapInstance.setView([latNum, lngNum], 12)
      const L = window.L
      if (mapMarker) {
        mapMarker.setLatLng([latNum, lngNum])
      } else {
        mapMarker = L.marker([latNum, lngNum]).addTo(mapInstance)
      }
      if (display_name) {
        mapMarker.bindPopup(display_name).openPopup()
      }
    }
  } catch (e) {
    console.error('[MapPicker] search failed:', e)
  }
}

watch(
  () => state.visible,
  (val) => {
    if (val) {
      searchQuery.value = ''
      if (mapMarker) {
        mapMarker.remove()
        mapMarker = null
      }
      nextTick(() => setTimeout(initMap, 100))
    }
  }
)

onUnmounted(() => {
  destroyMap()
})
</script>

<style lang="scss">
.map-picker-overlay {
  position: fixed;
  inset: 0;
  z-index: 3000;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgb(0 0 0 / 45%);
}

.map-picker-dialog {
  width: 800px;
  max-width: 90vw;
  border-radius: $radius-lg;
  background: $surface;
  box-shadow: $shadow;
  overflow: hidden;
}

.map-picker-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  padding: 14px 14px 14px 22px;
  border-bottom: $border;
  background: $surface-2;
}

.map-picker-title {
  display: flex;
  align-items: baseline;
  gap: 10px;
  min-width: 0;
  color: $text;
  font-size: 15px;
  font-weight: 600;
}

.map-picker-coord {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: $text-3;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12px;
  font-weight: 400;
}

.map-picker-close {
  display: grid;
  place-items: center;
  width: 28px;
  height: 28px;
  flex: 0 0 auto;
  padding: 0;
  border: 0;
  border-radius: $radius-sm;
  color: $text-3;
  background: none;
  cursor: pointer;
  font-size: 15px;
  transition:
    color $ease,
    background-color $ease;

  &:hover {
    color: $text;
    background: $surface-3;
  }
}

.map-picker-body {
  position: relative;
  padding: 16px;
}

.map-picker-search {
  position: absolute;
  top: 26px;
  left: 26px;
  z-index: 1000;
  width: 280px;
}

.map-picker-tip {
  margin: 10px 0 0;
  color: $text-3;
  font-size: 12px;
}

.map-picker-map {
  width: 100%;
  height: 460px;
  border: $border;
  border-radius: $radius;
  overflow: hidden;

  // 覆盖 leaflet 默认的 #ddd 容器底色，否则深色下瓦片间隙与加载中会透出浅灰
  &.leaflet-container {
    background: $surface-3;
  }
}

// 深色模式下把 OSM 瓦片反相压暗，避免大面积白底刺眼。
// 只作用于瓦片层，标记与弹窗保持原色。
.map-picker-map.dark .leaflet-tile-pane {
  filter: invert(1) hue-rotate(180deg) brightness(0.92) contrast(0.9) saturate(0.7);
}
</style>
