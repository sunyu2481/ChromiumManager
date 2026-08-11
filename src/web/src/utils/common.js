const validateForm = (formRef) => {
  return new Promise((resolve) => {
    formRef.validate((valid) => {
      resolve(valid)
    })
  })
}

// cssPx 读取根元素上的 CSS 长度变量并转成数字，避免 JS 侧重复硬编码布局尺寸。
// 变量名传不含 --cm- 前缀的部分，如 cssPx('row-h') 对应 --cm-row-h。
const cssPx = (name, fallback = 0) => {
  const raw = getComputedStyle(document.documentElement).getPropertyValue(`--cm-${name}`)
  const value = parseFloat(raw)
  return Number.isFinite(value) ? value : fallback
}

export { validateForm, cssPx }
