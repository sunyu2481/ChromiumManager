import { ElMessage } from 'element-plus'

const service = axios.create({
  baseURL: '',
  timeout: 5000,
  headers: {
    'Content-Type': 'application/json'
  }
})

// 响应拦截器
service.interceptors.response.use(
  (res) => {
    if (res.data.code === 200) {
      return res.data.data
    } else {
      return Promise.reject(new Error(res.data.message))
    }
  },
  (err) => {
    const msg = err.message?.includes('timeout') ? '请求超时！' : '连接服务失败！'
    ElMessage({ type: 'error', showClose: true, message: msg })
    err.silent = true
    return Promise.reject(err)
  }
)

export default service
