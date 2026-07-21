window.APP_DEFAULT_CONFIG = {
  DRAWER_MAX_DEEP: 6, // 右滑框栈最大深度
  VTAP_HIDE: false, // 甜橙控制采集器页面隐藏
  TO_DASHBOARD_PATH: '/dashboard', // 指定顶部图标跳转路径,默认应该是/dashboard
  SAAS_TENANT_TO_DASHBOARD_PATH: 'https://deepflow.io', // saas环境下租户指定顶部链接
  EXTERNAL_LINK: true, // 外部链接功能是否对外开发
  API_TIMEOUT: (60 + 3) * 10000, // 请求API超时时间, 默认为底层超时时间(默认60s) + 3s，确保超时状态也能从后端取到数据
  API_UPLOAD_TIMEOUT: 60 * 1000, // 请求上传相关API超时时间
  MINIMUM_BROWSER_VERSION: 96, // 浏览器最低版本
  MAX_LABEL_NUM: 100,
  MAX_LABEL_LENGTH: 32,
  SHOW_I18N: !false, // 是否显示i18n
  AES_CONFIG: {
    // aes配置需与后端保持一致，意义不大
    key: 'JeF8U9wHFOMfs2Y8',
    iv: '1234567890123456'
  },
  SM4_CONFIG: {
    key: 'JeF8U9wHFOMfs2Y8',
    mode: 'ecb',
    cipherType: 'base64'
  },
  FORCE_REPORT: true, // 回溯是否支持force_report状态配置
  ADMIN_TENANT_VISIBLE: true, // 默认管理员可见租户列表，改为false后不可见
  FLOW_VIEW_MULTIPLE_SELECT_LEN: 25, // 全景图多选下拉框最多可选数目
  FLOW_VIEW_HISTORY_SEARCH_BTN_NUM: 8, // 全景图历史搜索快捷按钮数目
  RESOURCE_AUTHORIZE_MULTIPLE_SELECT_LEN: 25, // 账户管理最多可给租户授权数目
  SELECT_DISPLAY_MAX_LEN: 100, // 下拉框最多展示条目数量
  SUPPORT_SECOND_DATA_TIME: 3600 * 36, // 距当前时间小于等于该值时视图支持秒级数据的选项,时间单位为s
  FLOW_VIEW_CUSTOM_LINE_CHART_NUM: 20, // 全景图支持自定义的折线数目
  VIEW_UPDATE: false, // 视图升级按钮，默认关闭
  WINDOW_SIZE: 60, // 折线图滑窗下拉框选项上限
  FLOW_VIEW_MAX_RESOURCE_SET: 10, // 全景图允许添加资源集的最大数目
  VIEW_TABLE_SELECT_MULTIPLE_LIMIT: 40, // 流日志table选择显示列下拉框最多可以选择的项目数
  SAFE_METHOD: false, // 开启安全方法过滤，替换请求方法
  DOMAIN_ATTACHED_K8S_COUNT: 10, // 云平台可添加k8sconfig的个数
  AUTO_REFRESH_SECOND_INTERVAL: [5, 10, 30], // 自动刷新秒级间隔配置（单位：秒），支持配置多个秒级选项
  FLOW_VIEW_KNOWLEDGE_NODE_COUNT: 2, // 全景图知识图谱拓扑图每类节点的展示个数
  DOCUMENT_TITLE: 'DeepFlow', // 页面title后缀
  COPYRIGHT: `Copyright © 2011-${new Date().getFullYear()} YUNSHAN Networks 版权所有`, // 版权信息
  CHART_COLOR: [
    // 图表颜色
    '#7FB80E',
    '#FFC20E',
    '#78CDD1',
    '#DF9464',
    '#D2553D',
    '#ECAEDD',
    '#09AB8C',
    '#F2D080',
    '#EA66A6',
    '#918597',
    '#FAB27B',
    '#6D94E1',
    '#DEAB8A',
    '#F3715C',
    '#65C294',
    '#F8ABA6',
    '#C77EB5',
    '#F2EADA',
    '#ECBDB9',
    '#76BECC',
    '#9B95C9',
    '#6F60AA',
    '#DEAB8A',
    '#AFB4DB',
    '#B8DBFF'
  ],
  CHART_ALARM_COLOR: 'rgba(255, 0, 0, 0.5)',
  TOPO_COLORS: {
    LINK_WITHDATA_DEFAULT: '#4C536E', // 流量线默认颜色
    LINK_AUTOFILLED_DEFAULT: 'green', // 自动添补的关系连线颜色
    LINK_MANUALFILLED_DEFAULT: 'green', // 手动添补的关系连线颜色
    NODE_OUTLINE_DEFAULT: '#4C536E', // 节点外圈颜色
    LINK_PHYSICAL_HIGHLIGHT: 'green' // 物理拓扑连线高亮颜色
  },
  // dagre 布局多组件间距比例（相对节点宽度），可手动调整
  TOPO_DAGRE_COMPONENT_SPACING_RATIO: 1,
  // dagre 布局坐标对齐步长（为 0 表示关闭）
  TOPO_DAGRE_ALIGN_STEP_X: 50,
  TOPO_DAGRE_ALIGN_STEP_Y: 50,
  // dagre 内部节点间距比例（相对节点宽 / 高, 注意对应到图里 x 是高度，y 是宽度，有个旋转）
  TOPO_DAGRE_NODE_SPACING_RATIO_X: 0.33,
  TOPO_DAGRE_NODE_SPACING_RATIO_Y: 0.2,
  // 需要隐藏的菜单路径
  HIDDEN_MENU_PATH_LIST: [],
  // sentry 错误上报 url
  // 空字符串: 不初始化 sentry
  // 非空字符串: 初始化 sentry 并上报访问信息
  SENTRY_DSN_URL: '',
  // 忘记密码链接地址
  FORGET_PASSWORD_LINK_URL: 'https://deepflow.io/phone-code.html',
  SIGNUP_LINK_URL: 'https://deepflow.io/signup.html',
  // 表格 size 配置
  // 可选值: 'mini', 'medium', 'large'
  // 主要影响: 行高, 文字大小
  // 作用范围: 应用, 网络下 列表
  VIEW_TABLE_SIZE: 'mini',
  // 表格 最大展示数量 配置
  // 超出此数量, 展示滚动条
  VIEW_TABLE_MAX_LEN: 100,
  // 是否启用登录页面的动画
  // 默认 true ： 启用动画
  IS_START_LOGIN_ANIMATION: true,
  // 时间轴视图是否开启过滤全0和全null数据
  FILTER_NULL_AND_ZERO: false,
  // 普通折线图, 修改图表展示 - 数据过滤 - 时间线数量 默认值
  NORMAL_LINES_CHART_SLIMIT: 20,
  // 子视图弹框显示宽度
  SUBVIEW_POPUP_WIDTH: '80vw',
  IS_KEEP_ALIVE_FOR_MODALS_TAB: true, // 页面右滑框tab是否开启缓存
  LOGIN_LIST_SORT: ['third_sso', 'deepflow', 'ldap', 'tce'], // 登录下拉列表的显示顺序
  // 只能使用 base64 转码后的图片
  LOGO_IMG: '',
  // AI plugin 分析页面最大节点数
  ANALYZE_NODES_MAX_LIMIT: 10,
  // AI plugin 分析页面最大连线数
  ANALYZE_LINKS_MAX_LIMIT: 20,
  // 指标页面和多查询条件的最大查询条件数量
  METRICS_EXPLORE_CONDITION_MAX: 10,
  // 指标页面和多查询条件的单个查询条件最大指标量数量
  METRICS_EXPLORE_METRICS_MAX: 10,
  // 指标页面同步url的最大长度
  SYNC_URL_MAX_LENGTH: 8000,
  // 登录地址，没有配置则使用系统自带的登录
  LOGIN_URL: '',
  FAVICON_IMG: '', // 使用 base64 转码后的 jpg图片
  // 是否隐藏 临时授权 的提示信息
  // true: 隐藏
  // false: 不隐藏 (显示)
  HIDE_PROVISIONAL_LICENSE_PROMPT: false,

  // 搜索组件 LRU 算法缓存长度
  SEARCH_COMPONENT_LRU_ALGORITHM_CACHE_LENGTH: 50,

  // 子视图的tools 默认显示
  CHART_TOOLS_DEFAULT_VISIBILITY: true,

  // 快速过滤组件展开的tag列表 单选的tag会默认展开
  FAST_FILTER_OPEN_TAG_LIST: [
    'response_status',
    'status',
    'event_level',
    'severity_number',
    'event_type'
  ],
  // 默认LLM模型
  DEFAULT_AI_MODEL: 'deepflow-engine-local',
  // iframe 的前缀地址 内部使用
  IFRAME_PREFIX_URL: '/df-web-core',
  // ai 告警分析跳转智能分析的地址
  ALERT_ANALYSIS_TO_AGENT_URL:
    '/ai/agent?q=`$业务名称`业务在`$时间范围`时间范围触发`$事件的告警策略名称`，请分析问题。',
  // 自定义平台信息
  CUSTOM_PLATFORM_INFO: {},
  // 是否开启邮件验证码功能
  ENABLE_EMAIL_VERIFY_CODE: false,
  // 隐藏用户二级目录（左下角用户位置）,默认都显示, Grafana 由上面的 EXTERNAL_LINK 控制
  // true 是隐藏, false 是显示
  HIDE_USER_SECOND_MENU: {
    团队管理: false,
    系统设置: false,
    平台信息: false,
    帮助: false
  },
  // 是否开启追踪功能
  ENABLE_TRACING: false,
  // 租户是否可以查看智能体菜单
  // 默认值为 false 不可见
  TENANT_AI_MENU_VISIBLE: false,
  // 是否为 TCE 环境
  IS_TCE: false,
  TCE_PLATFORM_TYPE: '',
  AI_DEFAULT_WORKFLOW_NAME: 'default',
  // 默认快速过滤组件的页大小, 注意只有不可数的资源过滤条件中才需要限制 page_size
  DEFAULT_FAST_LIST_PAGE_SIZE: 100,
  // 邀请成员级联字段配置
  // 配置级联选择器的字段顺序，最后一级始终是用户
  // 默认：['COMPANY', 'DEPARTMENT', 'SUB_DEPARTMENT'] - 公司 > 部门 > 子部门 > 用户
  // 可选：['COMPANY', 'STATE'] - 公司 > 状态 > 用户
  // 可选：['DEPARTMENT'] - 部门 > 用户
  // 空数组或不配置，则使用默认配置
  INVITE_MEMBER_CASCADER_FIELDS: ['COMPANY', 'DEPARTMENT', 'SUB_DEPARTMENT'],
  // v6 业务墙匹配，支持配置 lcuuid 和跳转域名
  // { domain: 'https://example.com/path', 业务名称-V6: { lcuuid: 'xxx' } } - 跳转到当前域名的指定页面
  V6_BUSINESS_WALL_MATCH: {},
  // 自定义页面配置
  // 用于动态扩展路由和菜单,支持加载外部 HTML 页面
  // 配置示例:
  //  {
  //     name: 'test-custom-page',
  //     path: '/custom',
  //     title: '自定义测试页',
  //     htmlUrl: '/assets/custom/test.html',
  //     order: 100,
  //     requiresAuth: true,
  //     hideInMenu: false
  //   },
  //   {
  //     name: 'test-custom-page2',
  //     path: '/custom2',
  //     title: '自定义测试页2',
  //     htmlUrl: '/assets/custom/test2.html',
  //     order: 100,
  //     requiresAuth: true,
  //     hideInMenu: false
  //   }
  CUSTOM_PAGES: [],
  // 是否显示 AI 子菜单「查询」
  // 默认 false 不显示；改为 true 后显示「查询」子菜单并开放 /ai/query 路由
  AI_QUERY_MENU_VISIBLE: false,
  // AI 子菜单「查询」的显示名称
  // 默认使用 i18n 的「数据查询」/ "Data Query"；配置为非空字符串时覆盖显示名称
  AI_QUERY_MENU_TITLE: '',
  CUSTOM_TABS_COOKIE_WHITELIST: ['sso_token']
}
