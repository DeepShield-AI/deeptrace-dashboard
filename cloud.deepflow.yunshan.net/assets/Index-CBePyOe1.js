import{Qt as e}from"./d3-vendor-Hbl8Isc-.js";import"./langium-vendor-BMu4Nv9e.js";import"./other-vendor-nQc8cw6U.js";import"./mergeAppConfig-DgJ9youI.js";import{$ as t,C as n,D as r,K as i,Lt as a,Nt as o,O as s,S as c,St as l,Y as u,Z as d,b as f,dt as p,k as m,m as h,n as g,st as _,t as v,wt as y,x as b,y as x}from"./@vue-vendor-BiAdlnhr.js";import{Dn as S,It as C,Lt as w,Mn as T,X as E,bn as D,on as O,q as k,vt as A,yt as j,zt as M}from"./vue-vendor-DnD1-op5.js";import"./datetime-vendor-DytgT_Kc.js";import"./ag-grid-vendor-DDlTIFNG.js";import{n as N}from"./api-DbeYCvWZ.js";import"./AgTable-CMX62WfL.js";import{r as P}from"./vueuse-vendor-BgqSJQ2H.js";import{a as F}from"./i18n-rDhRzd_D.js";import{t as I}from"./_plugin-vue_export-helper-CRt-r6Cj.js";import"./logger-tC2QQT1x.js";import"./tool-DPgdjGJj.js";import"./unit-CzQMEyJi.js";import"./useDeleteConfirm-CWRXp-7P.js";import"./time-Bqe8w6Gx.js";import"./algorithm-vendor-F4PoJiD8.js";import"./crypto-vendor-CP3gkDJw.js";import"./data-vendor-Bni7qkoM.js";import"./user-CFq8J9ex.js";import{dt as L}from"./index-BV1ABqqp.js";import{t as R}from"./helper-Ci5p3cN1.js";import{t as z}from"./moreContent-Bq_eHtL5.js";var B=v();g();var V={class:`add-knowledge-form`},H={class:`border-color-1 flex justify-end gap-3 border-t pt-4`},U=I(m({__name:`KnowledgeModal`,props:{visible:{type:Boolean},record:{}},emits:[`update:visible`,`add`,`edit`],setup(t,{emit:c}){let m=t,g=c,v=y([]),E=y(),k=async()=>{v.value=e(await N(`/ai/task-dimensions`,{method:`GET`}),e=>({label:e.name,value:e.name}))},M=l({knowledge:``,task_ids:[]}),P=y(!1),I={knowledge:[{required:!0,message:F(`请输入告警信息`)}],task_ids:[{required:!0,type:`array`,min:1,message:F(`请至少选择一个分析维度`)}]},L=x({get:()=>m.visible,set:e=>g(`update:visible`,e)}),R=x(()=>m.record?F(`编辑知识`):F(`新增知识`));_(()=>[m.visible,m.record],([e,t])=>{e&&t&&typeof t==`object`&&(M.knowledge=t.knowledge,M.task_ids=[...t.task_ids],E.value?.clearValidate())},{immediate:!0});let z=()=>{M.knowledge=``,M.task_ids=[],E.value?.clearValidate()},B=()=>{L.value=!1,z()},U=async()=>{try{if(await E.value?.validate())return;P.value=!0;let e={knowledge:M.knowledge,task_ids:M.task_ids};m.record?.knowledge_id?(await N(`/ai/knowledge/update`,{method:`post`,data:{...e,knowledge_id:m.record?.knowledge_id}}),j.success(F(`编辑知识成功`)),g(`edit`)):(await N(`/ai/knowledge/create`,{method:`post`,data:e}),j.success(F(`新增知识成功`)),g(`add`)),L.value=!1,z()}catch{j.error(m.record?F(`编辑知识失败，请重试`):F(`新增知识失败，请重试`))}finally{P.value=!1}};return i(()=>{k()}),(e,t)=>{let i=S,c=w,l=D,m=O,g=C,_=T,y=A;return u(),b(y,{visible:L.value,"onUpdate:visible":t[2]||=e=>L.value=e,"title-align":`start`,title:R.value,width:`600px`,"mask-closable":!1,footer:!1,onCancel:B},{default:p(()=>[f(`div`,V,[s(g,{ref_key:`formRef`,ref:E,model:M,rules:I,layout:`vertical`},{default:p(()=>[s(c,{label:o(F)(`告警信息`),field:`knowledge`},{default:p(()=>[s(i,{modelValue:M.knowledge,"onUpdate:modelValue":t[0]||=e=>M.knowledge=e,placeholder:o(F)(`请输入告警信息`)},null,8,[`modelValue`,`placeholder`])]),_:1},8,[`label`]),s(c,{label:o(F)(`分析维度列表`),field:`task_ids`},{default:p(()=>[s(m,{modelValue:M.task_ids,"onUpdate:modelValue":t[1]||=e=>M.task_ids=e,placeholder:o(F)(`请选择分析维度列表`),multiple:``,"allow-clear":``,class:`w-full`},{default:p(()=>[(u(!0),n(h,null,d(v.value,e=>(u(),b(l,{key:e.value,value:e.value},{default:p(()=>[r(a(e.label),1)]),_:2},1032,[`value`]))),128))]),_:1},8,[`modelValue`,`placeholder`])]),_:1},8,[`label`])]),_:1},8,[`model`]),f(`div`,H,[s(_,{onClick:B},{default:p(()=>[r(a(o(F)(`取消`)),1)]),_:1}),s(_,{type:`primary`,loading:P.value,onClick:U},{default:p(()=>[r(a(o(F)(`确定`)),1)]),_:1},8,[`loading`])])])]),_:1},8,[`visible`,`title`])}}}),[[`__scopeId`,`data-v-8cb64590`]]);g();var W={class:`flex h-full items-center gap-2 py-2`},G=m({__name:`OperationCellRenderer`,props:{params:{type:Object,required:!0},onEdit:Function,onDelete:Function},setup(e){let t=e,{t:r}=(0,B.useI18n)(),i=e=>{t.onEdit?t.onEdit(e):t.params.onEdit&&t.params.onEdit(e)},a=e=>{t.onDelete?t.onDelete(e):t.params.onDelete&&t.params.onDelete(e)};return(t,c)=>{let l=T,d=M;return u(),n(`div`,W,[s(d,{content:o(r)(`编辑`)},{default:p(()=>[s(l,{type:`text`,onClick:c[0]||=t=>i(e.params.data)},{icon:p(()=>[s(o(k),{class:`flex h-3 w-3 cursor-pointer items-center`})]),_:1})]),_:1},8,[`content`]),s(d,{content:o(r)(`删除`)},{default:p(()=>[s(l,{type:`text`,onClick:c[1]||=t=>a(e.params.data)},{icon:p(()=>[s(o(E),{class:`text-danger-6 flex h-3 w-3 cursor-pointer items-center`})]),_:1})]),_:1},8,[`content`])])}}});g();var K={class:`enhanced-analysis-container bg-1 flex h-full flex-col p-3`},q={class:`flex-1`},J={class:`flex h-full flex-col rounded`},Y={class:`flex-1`},X={class:`group text-title-2 ml-2 flex items-center gap-1 text-xs`},Z={class:`flex items-center gap-2`},Q=`# 运维案例分析提示词
## 角色定义
你是一个专业的运维案例分析专家，负责将用户提供的运维故障案例、问题描述或技术文档，梳理提取出标准化的告警信息和分析维度。

## 分析目标
从用户提供的案例中提取两个核心要素：
1. **告警信息** - 问题的基本特征和表现
2. **分析维度** - 解决问题时使用的分析方法和工具

## 输出格式
请严格按照以下YAML格式输出：
\`\`\`yaml
# 告警信息
alert_info:
  title: t('简洁的问题描述')
  severity: "critical|major|minor|warning"
  symptoms:
    - t('具体现象1')
    - t('具体现象2')
  error_codes: [t('具体错误代码')]
  metrics_anomaly:
    - metric: t('指标名称')
      normal_value: t('正常值')
      abnormal_value: t('异常值')

# 分析维度
analysis_dimensions:
  - name: t('维度名称')
    description: t('维度功能和目标的详细描述')
    sequence: 1
    effectiveness: "high|medium|low"
    analysis_logic: t('该维度的分析思路和逻辑过程')
    output_description: t('该维度分析后的预期输出和结果类型')
    tools:
      - tool_name: t('工具名称')
        tool_logic: t('工具分析逻辑')
        expected_output: t('预期输出结果')
        key_findings: [t('关键发现1'), t('关键发现2')]
  - name: t('维度名称')
    description: t('维度功能和目标的详细描述')
    sequence: 2
    effectiveness: "high|medium|low"
    analysis_logic: t('该维度的分析思路和逻辑过程')
    output_description: t('该维度分析后的预期输出和结果类型')
    tools:
      - tool_name: t('工具名称')
        tool_logic: t('工具分析逻辑')
        expected_output: t('预期输出结果')
        key_findings: [t('关键发现1'), t('关键发现2')]
\`\`\`

## 分析指导原则
### 告警信息提取
- **title**: 提炼问题核心，不超过20字
- **severity**: 根据业务影响评估严重程度
- **symptoms**: 提取可观测的具体现象，避免主观判断
- **error_codes**: 提取具体的错误代码、状态码或异常信息
- **metrics_anomaly**: 量化异常指标的正常值和异常值对比

### 分析维度识别
- **深度分析维度**: 重点关注为了达到深度分析而采用的技术手段和方法
- **排除基础维度**: 不需要特别说明服务定位、时间范围定位等基础步骤
- **工具组合使用**: 识别案例中实际使用的工具组合和分析深度，优先使用标准工具集
- **技术深挖过程**: 提炼案例中从表象到根因的技术分析路径
- **扩展工具补充**: 当标准工具集无法覆盖案例中的分析方法时，合理增加扩展工具

### 分析维度字段说明
- **name**: 维度的简洁名称，体现分析的核心领域
- **description**: 详细描述该维度的分析目标、适用场景和解决的问题类型
- **analysis_logic**: 该维度采用的分析思路、方法论和逻辑推理过程
- **output_description**: 该维度完成分析后产生的结果类型、数据格式和关键信息
- **tools**: 该维度中使用的具体工具及其应用方式

### 重点分析维度参考
- **调用链路分析**: 调用链路、服务依赖的深度分析
- **性能剖析分析**: CPU、内存、网络、存储的细粒度性能分析
- **基础资源分析**: 系统资源使用情况和瓶颈分析
- **网络通信分析**: 网络连接、传输质量的专项分析
- **应用行为分析**: 应用日志、文件IO、配置变更等应用层分析

## 工具识别和扩展原则
### 优先使用标准工具集
从以下预定义工具中优先选择合适的分析工具：

**深度分析工具** (重点关注):
- **全栈路径追踪时延瓶颈**: 分析链路中具体瓶颈位置，区分网络/系统/应用瓶颈
- **性能剖析数据分析**: 查询服务实例的CPU剖析数据，定位高消耗进程和热点函数
- **基础指标数据分析**: 监控CPU、内存、磁盘指标突增和接近上限情况
- **网络性能指标分析**: 检测网络重传、零窗等网络质量问题
- **应用超时比例分析**: 分析服务间调用的超时比例和趋势
- **网络异常类型分析**: 分析调用日志的网络异常类型（如客户端重置）
- **网络TCP连接错误分析**: 检测网络建连失败情况
- **调用日志分析**: 分析客户端异常、服务端异常及超时的调用日志
- **资源变更事件分析**: 查询服务实例的资源变更事件，按类型统计分析
- **应用日志分析**: 统计服务实例的ERROR或WARN日志信息
- **文件读写事件分析**: 分析服务的文件读写性能，识别IO瓶颈

**基础定位工具** (不作为重点):
- **黄金指标定位瓶颈时间范围**: 分析入口路径响应时延变化，确定异常时间窗口
- **黄金指标定位异常时间范围**: 分析异常服务的异常比例指标变化时间范围
- **业务拓扑追踪时延瓶颈**: 识别自身消耗时延最高的瓶颈服务及其上下游关系
- **业务拓扑查找异常源头**: 查找访问路径中层级最大的异常服务，定位根因

### 扩展工具识别
当案例中出现标准工具集未覆盖的分析方法时，可以增加扩展工具：

**常见扩展工具类型**:
- **采集器/探针状态分析**: 监控观测系统自身组件的运行状态
- **主机/节点流量分析**: 从主机维度的流量分布和异常分析
- **协议层分析**: HTTP/TCP/UDP等协议层面的详细分析
- **容器运行时分析**: 容器镜像拉取、启动、资源分配等操作分析
- **存储IO详情分析**: 磁盘、文件系统的深度IO性能分析
- **进程/线程状态分析**: 进程级别的资源使用和状态监控
- **数据库连接池分析**: 数据库连接管理和性能分析
- **消息队列状态分析**: MQ连接、消费、堆积等状态分析
- **负载均衡器分析**: LB的转发策略、健康检查、流量分布分析

### 扩展工具命名规范
扩展工具应标注为"（扩展工具）"并遵循以下命名规范：
- 功能描述 + 分析层级 + （扩展工具）
- 示例：HTTP访问详情分析（扩展工具）、容器镜像拉取分析（扩展工具）

## 重点关注
- **深度分析过程**: 重点提取为了深入分析问题而采用的技术手段
- **技术分析路径**: 关注从表象到根因的技术深挖过程
- **工具选择原则**: 优先从标准工具集中选择，必要时合理扩展
- **扩展工具使用**: 当案例涉及标准工具集未覆盖的分析方法时，增加扩展工具并标注
- **排除基础步骤**: 不需要专门说明服务定位、时间范围确定等基础操作

## 特殊情况处理
- **信息不足**: 基于常见故障模式进行合理补充
- **多重问题**: 按主要问题进行分析，次要问题在维度中体现
- **复合场景**: 拆分为多个独立的告警信息和分析维度

## 处理原则
1. **标准优先**: 优先使用预定义的标准工具集进行分析
2. **合理扩展**: 当案例中的分析方法超出标准工具集时，合理增加扩展工具
3. **标准化**: 使用统一的术语和分类标准，扩展工具需明确标注
4. **实用性**: 重点关注可复用的分析方法和经验
5. **完整性**: 确保涵盖案例中所有有价值的技术分析手段

---
现在请提供您的运维案例，我将按照上述规范进行分析和梳理。`,$=m({__name:`Index`,setup(e,{expose:i}){let{locale:l}=(0,B.useI18n)(),d=y(),m=y(!1),h=y(!1),g=y(null),_=y(!1),{copy:v}=P({legacy:!0}),x=async()=>{try{await v(Q),j.success(F(`提示词已复制到剪贴板`))}catch{j.error(F(`复制失败`))}},S=y([{key:`introduction`,title:F(`知识库体系介绍`),content:`# 强化分析维度推理知识库体系介绍

## 核心定位

强化分析维度推理知识库是运维智能体进行**智能维度选择**的专用知识引擎。它专门为解决t('面对具体故障时，如何选择最优的分析维度组合')这一核心问题而设计，通过结构化的历史案例和专家经验，指导智能体做出精准的维度推理决策。

## 主要功能

### 🎯 维度选择推理支撑
当运维智能体接收到新的故障告警时，知识库通过**相似案例匹配算法**，快速检索出与当前故障特征相似的历史案例，分析这些案例中使用的分析维度组合，为智能体推荐最适合的维度选择策略。

### 📊 成功模式识别
知识库系统性地记录每个历史案例中**使用的分析维度**、**分析顺序**、**解决效果**等关键信息，通过统计分析识别出针对特定问题类型的成功分析模式，形成可复用的维度选择经验。

### 🔍 智能匹配引擎
基于故障的**技术栈特征**、**问题类型**、**环境特点**等多维度信息，智能匹配最相关的历史案例。通过特征向量化和相似性计算，确保推荐的维度选择策略具有高度的相关性和有效性。

## 知识库结构设计

### 案例维度映射
每个知识库条目都详细记录了：
- **使用的分析维度组合**：哪些维度参与了分析
- **维度分析顺序**：先用哪个维度，后用哪个维度
- **各维度的有效性**：每个维度在该案例中的贡献度
- **维度协同效果**：多维度组合分析的整体效果

### 特征标签体系
建立了完整的标签分类体系：
- **技术维度标签**：技术栈、架构、部署方式等
- **问题维度标签**：故障类型、影响范围、时间模式等
- **环境维度标签**：生产环境、云平台、基础设施等

### 检索优化机制
通过标准化的关键词体系和特征向量，支持：
- **精确匹配**：基于技术栈和问题类型的精确检索
- **模糊匹配**：基于相似性算法的智能推荐
- **模式识别**：识别成功的维度选择模式

## 核心工作流程

### 1. 故障特征提取
智能体接收故障告警后，自动提取：
- 告警基本信息（类型、组件、严重程度）
- 技术环境特征（技术栈、架构模式）
- 问题特征描述（症状、错误信息、异常指标）

### 2. 相似案例检索
基于提取的特征信息，在知识库中检索：
- **技术栈匹配度>80%**的相似案例
- **问题特征相似度>70%**的历史故障
- **环境特点一致**的成功案例

### 3. 维度推荐生成
分析匹配到的案例，生成维度推荐：
- **主要维度**：成功率最高的核心分析维度
- **辅助维度**：提供补充验证的次要维度
- **分析顺序**：基于历史经验的最优分析顺序
- **并行策略**：可以同时进行的维度组合

### 4. 策略优化反馈
根据实际分析效果，持续优化推理策略：
- 记录维度选择的成功率
- 更新相似性计算权重
- 完善维度推荐规则

## 知识来源与更新

### 历史案例积累
- **存量案例规整化**：将现有的故障处理文档规整为标准格式
- **专家经验提取**：从资深运维专家的分析过程中提取维度选择经验
- **工具使用记录**：记录不同分析工具在各类问题中的有效性

### 持续学习机制
- **新案例自动入库**：每个新的故障案例都会自动规整化并加入知识库
- **成功模式更新**：基于最新的成功案例更新维度选择模式
- **失效模式识别**：识别并淘汰过时或无效的分析策略

## 应用价值

### 提升分析精度
通过历史成功经验指导，将维度选择的准确率从经验判断的60-70%提升到基于数据驱动的85%以上，显著减少分析方向错误导致的时间浪费。

### 加速故障定位
基于知识库推荐的维度组合，智能体能够快速聚焦到最有可能发现问题的分析方向，将故障定位时间从传统的数小时缩短到分钟级别。

### 降低专业门槛
将专家级的维度选择经验固化到知识库中，使得即使是经验较少的运维人员也能获得专家级的分析指导，提升整体运维团队的能力水平。

### 确保分析全面性
通过系统化的维度推荐，避免因经验局限或分析盲区导致的重要分析角度遗漏，确保故障分析的全面性和准确性。

---

**强化分析维度推理知识库**是运维智能体实现从t('经验驱动')向t('数据驱动')转变的关键基础设施，通过结构化的知识管理和智能化的推理机制，为故障诊断提供科学、精准、高效的维度选择指导。`},{key:`knowledge`,title:F(`如何生成知识`),content:`# 基于大模型的知识库生成说明

## 生成机制概述

利用顶级大语言模型（如 DeepSeek、Claude、GPT-4 等）的强大理解和结构化能力，将企业过往积累的非结构化运维案例自动转换为标准化知识库条目。通过精心设计的<span class="bg-fill-2 px-2 h-4 w-[74px] rounded"><span class="text-title-1">提示词工程</span>&nbsp;&nbsp;&nbsp;&nbsp;</span>，大模型能够深度理解故障文档的技术内容，提取关键信息，并按照规范格式生成高质量的结构化知识。`}]),C=L(async e=>{let t={knowledge_list:[],total:0};return t=e.search?.keyword?await N(`/ai/knowledge/search`,{method:`get`,urlQuery:{text:e.search?.keyword,limit:e.pagination.pageSize,offset:(e.pagination.currentPage-1)*e.pagination.pageSize}}):await N(`/ai/knowledge`,{method:`get`,params:{limit:e.pagination.pageSize,offset:(e.pagination.currentPage-1)*e.pagination.pageSize}}),{data:t.knowledge_list,total:t?.total||0}}),w=e=>{g.value=e,m.value=!0},E=()=>{d.value?.refresh()},D=()=>{d.value?.refresh()},O=e=>{A.confirm({title:F(`删除`),width:464,titleAlign:`start`,modalClass:`delete-confirm-modal`,content:F(`确定要删除这条知识吗？`),onOk:async()=>{_.value=!0;try{await N(`/ai/knowledge/delete`,{method:`post`,data:{knowledge_id:e.knowledge_id}}),j.success(F(`删除成功`)),d.value?.refresh()}catch{j.error(F(`删除失败`))}finally{_.value=!1}},okButtonProps:{type:`primary`,status:`danger`},okText:F(`确认删除`),cancelText:F(`取消`)})},k=async()=>{try{let e=await N(`/ai/knowledge/export`,{method:`GET`}),t=JSON.stringify(e,null,2),n=new Blob([t],{type:`application/json`}),r=window.URL.createObjectURL(n),i=document.createElement(`a`);i.href=r,i.download=`konwledge_${new Date().toISOString().split(`T`)[0]}.json`,document.body.appendChild(i),i.click(),document.body.removeChild(i),window.URL.revokeObjectURL(r),j.success(F(`导出成功`))}catch{j.error(F(`导出失败`))}},M=()=>{g.value=null,m.value=!0},I=[{headerName:F(`告警信息`),field:`knowledge`,minWidth:500,sortable:!1,cellRenderer:e=>`<span class="text-title-1">${e.value}</span>`},{headerName:F(`分析维度列表`),field:`task_ids`,minWidth:500,sortable:!1,cellRenderer:`TagCellRenderer`,cellRendererParams:{tagField:`task_ids`},tooltipField:`task_ids`,tooltipComponentParams:{showTags:!0}},{headerName:F(`更新时间`),field:`updated_at`,minWidth:500,sortable:!0,cellRenderer:e=>`<span class="text-title-2">${R(e.value)}</span>`},{headerName:F(`操作`),field:`actions`,width:l.value===`en-US`?105:80,maxWidth:l.value===`en-US`?105:80,sortable:!1,pinned:`right`,cellRenderer:`OperationCellRenderer`,cellRendererParams:{onEdit:w,onDelete:O}}];return i({OperationCellRenderer:G}),(e,i)=>{let l=t(`YsIcon`),_=T,v=t(`YsAsyncTable`);return u(),n(`div`,K,[f(`div`,q,[f(`div`,J,[f(`div`,Y,[s(v,{ref_key:`tableRef`,ref:d,columns:I,"data-source":o(C),searchable:!0,"search-placeholder":o(F)(`搜索`)},{left:p(()=>[f(`div`,X,[s(l,{icon:`common-hints-question`,class:`group-hover:text-primary-6`}),f(`span`,{class:`group-hover:text-primary-6 cursor-pointer`,onClick:i[0]||=e=>h.value=!0},a(o(F)(`如何丰富知识库`)),1)])]),operation:p(()=>[f(`div`,Z,[s(_,{size:`medium`,onClick:k},{default:p(()=>[s(l,{icon:`common-actions-export`}),r(` `+a(o(F)(`导出知识库`)),1)]),_:1}),s(_,{type:`primary`,size:`medium`,onClick:M},{icon:p(()=>[s(l,{icon:`ai-plus`})]),default:p(()=>[r(` `+a(o(F)(`新增知识`)),1)]),_:1})])]),_:1},8,[`data-source`,`search-placeholder`])])])]),m.value?(u(),b(U,{key:0,visible:m.value,"onUpdate:visible":i[1]||=e=>m.value=e,record:g.value,onAdd:E,onEdit:D},null,8,[`visible`,`record`])):c(``,!0),s(z,{visible:h.value,"onUpdate:visible":i[2]||=e=>h.value=e,tabs:S.value,"default-active-tab":`introduction`,"copy-prompt-content":x},null,8,[`visible`,`tabs`])])}}});export{$ as default};