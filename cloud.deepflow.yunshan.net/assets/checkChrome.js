function isChrome() {
  const userAgent = navigator.userAgent.toLowerCase()
  if (userAgent.indexOf('chrome') > -1 && userAgent.indexOf('edg') === -1) {
    const raw = userAgent.match(/chrom(e|ium)\/([0-9]+)\./)
    if (raw && Number(raw[2]) >= APP_DEFAULT_CONFIG.MINIMUM_BROWSER_VERSION) {
      return true
    } else {
      return false
    }
  } else {
    return false
  }
}

const BROWSER_HINT_STORAGE_KEY = 'deepflow_browser_version_hint_disabled'

function getShouldSkipBrowserHint() {
  try {
    return localStorage.getItem(BROWSER_HINT_STORAGE_KEY) === '1'
  } catch (error) {
    return false
  }
}

function setSkipBrowserHint() {
  try {
    localStorage.setItem(BROWSER_HINT_STORAGE_KEY, '1')
  } catch (error) {
    // localStorage 可能不可用，忽略存储失败但不影响关闭提示。
  }
}

function renderBrowserHint() {
  document.body.insertAdjacentHTML(
    'beforeend',
    `<div id="chromeMsg" style="color: rgb(230, 162, 60); position: fixed; top: 10px; right: 10px;
    width: 340px;
    background: rgb(253, 246, 236);
    padding: 15px 20px;
    border-radius: 4px;
    border: 1px solid rgb(250, 236, 216);
    z-index: 9999;
    font-size: 14px;
    line-height: 1.6;">
      <div style="display: flex; align-items: start; justify-content: space-between; gap: 8px;">
        <span>
          您当前使用非 DeepFlow 推荐浏览器，为保证您的正常使用，请切换到 Chrome 浏览器
          ${APP_DEFAULT_CONFIG.MINIMUM_BROWSER_VERSION} 以上版本。
        </span>
        <button id="chromeMsgCloseBtn" type="button" style="border: none; background: transparent; color: rgb(230, 162, 60); font-size: 16px; cursor: pointer; line-height: 1;">×</button>
      </div>
      <label style="display: inline-flex; align-items: center; margin-top: 10px; cursor: pointer; color: rgb(144, 147, 153);">
        <input id="chromeMsgSkipCheckbox" type="checkbox" style="margin-right: 6px;" />
        不再提示
      </label>
    </div>`
  )

  const closeBtn = document.getElementById('chromeMsgCloseBtn')
  const checkbox = document.getElementById('chromeMsgSkipCheckbox')
  const container = document.getElementById('chromeMsg')

  closeBtn?.addEventListener('click', () => {
    if (checkbox instanceof HTMLInputElement && checkbox.checked) {
      setSkipBrowserHint()
    }
    container?.remove()
  })
}

if (!isChrome() && !getShouldSkipBrowserHint()) {
  renderBrowserHint()
}
