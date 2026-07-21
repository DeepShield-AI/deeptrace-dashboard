import{Et as e,Pt as t,Qt as n}from"./d3-vendor-Hbl8Isc-.js";import"./langium-vendor-BMu4Nv9e.js";import"./other-vendor-nQc8cw6U.js";import"./mergeAppConfig-DgJ9youI.js";import{$ as r,C as i,D as a,K as o,Lt as s,Nt as c,O as l,S as u,St as d,Y as f,Z as p,b as m,dt as h,k as g,m as _,n as v,st as y,t as b,wt as x,x as S,y as C}from"./@vue-vendor-BiAdlnhr.js";import{Bt as w,Ct as T,Dn as E,It as D,Lt as O,Mn as k,X as A,bn as j,cn as M,ln as N,on as P,q as F,vt as I,yt as L,zt as R}from"./vue-vendor-DnD1-op5.js";import"./datetime-vendor-DytgT_Kc.js";import{n as z}from"./api-DbeYCvWZ.js";import{r as B}from"./vueuse-vendor-BgqSJQ2H.js";import{a as V}from"./i18n-rDhRzd_D.js";import{t as H}from"./_plugin-vue_export-helper-CRt-r6Cj.js";import"./logger-tC2QQT1x.js";import"./tool-DPgdjGJj.js";import"./unit-CzQMEyJi.js";import"./algorithm-vendor-F4PoJiD8.js";import"./crypto-vendor-CP3gkDJw.js";import"./data-vendor-Bni7qkoM.js";import"./user-CFq8J9ex.js";import"./config-BEQP1bGJ.js";import"./recommendMetrics-EoJbuzUB.js";import{t as ee}from"./tools-D6Ne3_AJ.js";import{t as te}from"./helper-Ci5p3cN1.js";import{t as ne}from"./moreContent-Bq_eHtL5.js";var re=b();v();var U={class:`add-dimension-form`},W={class:`border-color-1 flex justify-end gap-3 border-t pt-4`},G=g({__name:`DimensionModal`,props:{visible:{type:Boolean},record:{}},emits:[`update:visible`,`add`,`edit`],setup(e,{emit:r}){let o=e,g=r,v=x([]),b=x();v.value=t(ee,(e,t)=>(e.push({label:t,value:t}),e),[]);let w=d({name:``,description:``,action:``,output:``,tools:[]}),A=x({name:[{required:!0,message:V(`请输入名称`)}],description:[{required:!0,message:V(`请输入维度描述`)}],action:[{required:!0,message:V(`请输入分析逻辑`)}],output:[{required:!0,message:V(`请输入输出文案`)}],tools:[{required:!0,message:V(`请选择调用工具`)}]}),M=x(!1),N=C({get:()=>o.visible,set:e=>g(`update:visible`,e)}),F=C(()=>o.record?V(`编辑维度`):V(`新增维度`));y([()=>o.visible,()=>o.record],([e,t])=>{e&&t&&(w.name=t.name||``,w.description=t.description||``,w.action=t.action||``,w.output=t.output||``,w.tools=n(t.tools,e=>e.name))},{immediate:!0});let R=()=>{w.name=``,w.description=``,w.action=``,w.output=``,w.tools=[],b.value?.clearValidate()},B=()=>{N.value=!1,R()},H=async()=>{try{if(await b.value?.validate())return;M.value=!0;let e={name:w.name,description:w.description,output:w.output,tools:n(w.tools,e=>({name:e}))};(o.record?.type===V(`自定义`)||!o.record?.uuid)&&(e.action=w.action),o.record?.uuid?(await z(`/ai/task-dimensions/update`,{urlQuery:{uuid:o.record?.uuid},data:e}),L.success(V(`编辑维度成功`)),g(`edit`)):(await z(`/ai/task-dimensions/create`,{method:`post`,data:e}),L.success(V(`新增维度成功`)),g(`add`)),N.value=!1,R()}catch{L.error(o.record?V(`编辑维度失败，请重试`):V(`新增维度失败，请重试`))}finally{M.value=!1}};return(e,t)=>{let n=E,r=O,d=T,g=j,y=P,x=D,C=k,L=I;return f(),S(L,{visible:N.value,"onUpdate:visible":t[5]||=e=>N.value=e,"title-align":`start`,title:F.value,"mask-closable":!1,width:`600px`,footer:!1,onCancel:B},{default:h(()=>[m(`div`,U,[l(x,{ref_key:`formRef`,ref:b,model:w,rules:A.value,layout:`vertical`},{default:h(()=>[l(r,{label:c(V)(`名称`),field:`name`,class:`required-field`,required:``},{default:h(()=>[l(n,{modelValue:w.name,"onUpdate:modelValue":t[0]||=e=>w.name=e,placeholder:c(V)(`请输入名称`)},null,8,[`modelValue`,`placeholder`])]),_:1},8,[`label`]),l(r,{label:c(V)(`维度描述`),field:`description`,class:`required-field`,required:``},{default:h(()=>[l(d,{modelValue:w.description,"onUpdate:modelValue":t[1]||=e=>w.description=e,placeholder:c(V)(`请输入描述`),"auto-size":{minRows:3,maxRows:5}},null,8,[`modelValue`,`placeholder`])]),_:1},8,[`label`]),o.record?.type===`自定义`||!o.record?.uuid?(f(),S(r,{key:0,label:c(V)(`分析逻辑`),field:`action`,class:`required-field`,required:``},{default:h(()=>[l(d,{modelValue:w.action,"onUpdate:modelValue":t[2]||=e=>w.action=e,placeholder:c(V)(`请输入分析逻辑`),"auto-size":{minRows:3,maxRows:5}},null,8,[`modelValue`,`placeholder`])]),_:1},8,[`label`])):u(``,!0),l(r,{label:c(V)(`输出文案`),field:`output`,class:`required-field`,required:``},{default:h(()=>[l(d,{modelValue:w.output,"onUpdate:modelValue":t[3]||=e=>w.output=e,placeholder:c(V)(`请输入文案`),"auto-size":{minRows:3,maxRows:5}},null,8,[`modelValue`,`placeholder`])]),_:1},8,[`label`]),l(r,{label:c(V)(`调用工具`),field:`tools`,class:`required-field`,required:``},{default:h(()=>[l(y,{modelValue:w.tools,"onUpdate:modelValue":t[4]||=e=>w.tools=e,placeholder:c(V)(`请选择工具`),"allow-clear":``,multiple:``,class:`w-full`,"trigger-props":{autoFitPosition:!1}},{default:h(()=>[(f(!0),i(_,null,p(v.value,e=>(f(),S(g,{key:e.value,value:e.value},{default:h(()=>[a(s(e.label),1)]),_:2},1032,[`value`]))),128))]),_:1},8,[`modelValue`,`placeholder`])]),_:1},8,[`label`])]),_:1},8,[`model`,`rules`]),m(`div`,W,[l(C,{onClick:B},{default:h(()=>[a(s(c(V)(`取消`)),1)]),_:1}),l(C,{type:`primary`,loading:M.value,onClick:H},{default:h(()=>[a(s(c(V)(`确定`)),1)]),_:1},8,[`loading`])])])]),_:1},8,[`visible`,`title`])}}});v();var K={class:`dimension-header`},q={class:`flex items-start justify-between`},J={class:`flex items-center gap-1`},Y={class:`text-title-1 mb-0 text-lg font-medium`},X={class:`dimension-body space-y-3`},ie={class:`dimension-section`},ae={class:`text-title-2 mb-1 text-xs`},oe={class:`bg-fill-1 rounded-md p-3`},se={class:`text-title-1 mb-0 text-sm leading-relaxed`},ce={key:0,class:`dimension-section`},le={class:`text-title-2 mb-1 text-xs`},ue={class:`bg-fill-1 rounded-md p-3`},de={class:`text-title-1 mb-0 text-sm leading-relaxed`},fe={class:`dimension-section`},pe={class:`text-title-2 mb-1 text-xs`},me={class:`bg-fill-1 rounded-md p-3`},he={class:`text-title-1 mb-0 text-sm leading-relaxed`},ge={class:`dimension-section`},_e={class:`text-title-2 mb-1 text-xs`},ve={class:`flex flex-wrap gap-2`},ye=g({__name:`DimensionViewDrawer`,props:{visible:{type:Boolean},record:{}},emits:[`update:visible`],setup(e,{emit:t}){let n=e,r=t,o=C({get:()=>n.visible,set:e=>r(`update:visible`,e)});return(t,n)=>{let r=N,d=w;return f(),S(d,{visible:o.value,"onUpdate:visible":n[0]||=e=>o.value=e,title:``,width:1e3,"mask-closable":!0,placement:`right`,footer:!1,"unmount-on-close":``},{title:h(()=>[m(`div`,K,[m(`div`,q,[m(`div`,J,[m(`h3`,Y,s(e.record?.name||`--`),1),l(r,{size:`small`,class:`bg-fill-2 text-title-1 ml-1 px-2 py-0.5 text-xs`},{default:h(()=>[a(s(e.record?.type||`--`),1)]),_:1})])])])]),default:h(()=>[m(`div`,X,[m(`div`,ie,[m(`h4`,ae,s(c(V)(`维度描述`)),1),m(`div`,oe,[m(`p`,se,s(e.record?.description||`--`),1)])]),e.record?.type===`自定义`?(f(),i(`div`,ce,[m(`h4`,le,s(c(V)(`分析逻辑`)),1),m(`div`,ue,[m(`p`,de,s(e.record?.action||`--`),1)])])):u(``,!0),m(`div`,fe,[m(`h4`,pe,s(c(V)(`输出文案`)),1),m(`div`,me,[m(`p`,he,s(e.record?.output),1)])]),m(`div`,ge,[m(`h4`,_e,s(c(V)(`关联工具`)),1),m(`div`,ve,[(f(!0),i(_,null,p(e.record?.tools,e=>(f(),S(r,{key:e.name,size:`small`,class:`bg-fill-2 text-title-1 px-2 py-0.5`},{default:h(()=>[a(s(e.name),1)]),_:2},1024))),128))])])])]),_:1},8,[`visible`])}}});v();var be={class:`flex h-full items-center gap-2 py-2`},Z=g({__name:`OperationCellRenderer`,props:{params:{type:Object,required:!0},onEdit:Function,onDelete:Function},setup(e){let t=e,n=e=>{t.onEdit?t.onEdit(e):t.params.onEdit&&t.params.onEdit(e)},r=e=>{t.onDelete?t.onDelete(e):t.params.onDelete&&t.params.onDelete(e)};return(t,a)=>{let o=k,s=R;return f(),i(`div`,be,[l(s,{content:c(V)(`编辑`)},{default:h(()=>[l(o,{type:`text`,onClick:a[0]||=t=>n(e.params.data)},{icon:h(()=>[l(c(F),{class:`flex h-3 w-3 cursor-pointer items-center`})]),_:1})]),_:1},8,[`content`]),l(s,{content:c(V)(`删除`)},{default:h(()=>[l(o,{type:`text`,onClick:a[1]||=t=>r(e.params.data)},{icon:h(()=>[l(c(A),{class:`text-danger-6 flex h-3 w-3 cursor-pointer items-center`})]),_:1})]),_:1},8,[`content`])])}}});v();var xe={class:`flex h-full w-full items-center`},Q={class:`flex max-w-full flex-nowrap gap-1 overflow-hidden bg-transparent text-ellipsis whitespace-nowrap`},$=H(g({__name:`ToolsCellRenderer`,props:{params:{type:Object,required:!0}},setup(e){let t=e,r=C(()=>n(t.params.data?.tools,e=>e.name)||[]);return(e,t)=>{let n=M;return f(),i(`div`,xe,[m(`div`,Q,[l(n,{"default-value":r.value,"max-tag-count":1,readonly:``},null,8,[`default-value`])])])}}}),[[`__scopeId`,`data-v-211a608c`]]);v();var Se={class:`dimension-orchestration-container bg-1 flex h-full flex-col p-3`},Ce={class:`flex items-center justify-between`},we={class:`group text-title-2 flex items-center gap-2 text-xs`},Te={class:`flex items-center gap-1`},Ee={class:`flex items-center gap-1`},De={class:`flex-1 py-3`},Oe={class:`flex h-full flex-col rounded`},ke={class:`flex-1`},Ae=`# 分析维度生成提示词

## 核心任务
你是运维智能体的分析维度设计专家，需要基于用户提供的知识库内容、技术场景和问题类型，设计新的分析维度或优化现有维度，确保智能体具备全面的故障诊断能力。重点从知识库中挖掘独特的分析方法、判断标准和实战经验。

## 设计原则

### 知识库驱动原则
- 优先从用户知识库中提取独特的分析方法和经验
- 将知识库中的故障案例转化为可复用的分析模式
- 识别知识库体现的技术栈特点和业务场景需求
- 结合知识库中的工具使用经验和最佳实践

### 专业性原则
- 每个维度必须有明确的技术领域专精
- 分析方法需要基于该领域的最佳实践
- 判断标准要符合行业标准和实际经验

### 独立性原则
- 维度之间避免功能重叠，各有明确边界
- 每个维度能够独立完成特定类型问题的分析
- 具备完整的分析工具链和输出标准

### 实用性原则
- 分析步骤要可执行，避免抽象理论
- 判断标准要量化，便于自动化执行
- 输出结果要actionable，直接指导修复

## 知识库分析要求

在设计维度前，请先深度分析用户提供的知识库内容：

### 技术栈识别
- 识别知识库中涉及的主要技术组件和架构模式
- 分析特定技术栈的故障特征和分析需求
- 发现知识库中体现的技术环境特点（云原生、传统架构、混合环境等）

### 分析方法提取
- 从故障案例中提取具体的分析步骤和排查思路
- 识别知识库中提到的分析工具使用方法和组合策略
- 提取专家在分析过程中的决策逻辑和判断标准

### 业务场景理解
- 分析知识库反映的业务需求和运维场景
- 识别特定行业或公司的运维特点和关注重点
- 发现知识库中体现的SLA要求和性能标准

### 经验模式总结
- 从多个案例中归纳共同的问题模式和解决思路
- 识别知识库中反复提到的关键指标和阈值
- 提取快速定位和排除问题的经验技巧

## 维度设计框架

请按以下结构设计分析维度：

### 基本信息
\`\`\`yaml
task: [维度名称]
desc: [维度功能描述 - 说明该维度解决什么问题，适用什么场景，核心价值是什么]
\`\`\`

### 分析行动
\`\`\`yaml
action: [详细的分析步骤序列，重点体现从知识库中学到的分析方法]
# 要求：
# 1. 优先使用知识库中验证有效的分析步骤
# 2. 结合知识库中的工具使用经验和技巧
# 3. 体现知识库中专家的分析优先级和逻辑顺序
# 4. 包含知识库中提到的关键判断节点和决策逻辑
# 5. 融入知识库中的异常情况处理经验
# 6. 引用知识库中的具体案例和经验教训
\`\`\`

### 输出规范
\`\`\`yaml
output: [分析结果的标准格式和内容要求]
# 要求：
# 1. 结果要结构化，便于后续处理
# 2. 包含问题定位、影响评估、修复建议
# 3. 提供量化的评估指标
\`\`\`

### 工具集成
\`\`\`yaml
tools: [该维度使用的分析工具列表]
# 要求：
# 1. 工具要与分析步骤对应
# 2. 优先使用现有的可观测性工具
# 3. 考虑工具的组合使用策略
\`\`\`

## 设计要求

### 技术深度
- 深度挖掘知识库中体现的技术领域专业知识
- 基于知识库案例总结该领域的核心分析方法
- 结合知识库内容反映最新的技术发展和工具能力

### 场景覆盖
- 基于知识库内容确定该维度覆盖的故障类型和问题场景
- 考虑知识库体现的环境特点（云原生、传统架构等）
- 体现知识库中案例反映的复杂系统交互理解

### 实战导向
- 分析步骤要基于知识库中的真实故障处理经验
- 判断标准要来源于知识库中的实际运维场景
- 修复建议要体现知识库中验证有效的解决方案

### 知识库适配
- 充分利用知识库中的独特分析方法和经验
- 体现知识库反映的技术栈特点和业务需求
- 融入知识库中专家的实战技巧和最佳实践

### 智能化水平
- 支持自动化执行的分析流程
- 包含智能决策和路径选择逻辑
- 具备学习和优化的能力设计

## 输出格式

请严格按照以下YAML格式输出：

\`\`\`yaml
- task: [维度名称]
  desc: [详细的功能描述，包括适用场景、解决的问题类型、核心价值]
  action: [完整的分析逻辑，每个步骤要具体可执行，包含工具使用方法、判断标准、决策逻辑]
  output: [标准化的输出格式，包括问题定位结果、影响评估、修复建议的具体内容要求]
  tools: [使用的分析工具列表，要与action中的步骤对应]
\`\`\`

## 重点关注

### 新兴技术场景
- 云原生和容器化环境的特有问题
- 微服务架构下的复杂交互分析
- AI/ML应用的性能和稳定性问题
- 边缘计算和IoT场景的运维挑战

### 跨领域整合
- 考虑多技术栈的综合影响
- 关注业务逻辑与技术实现的关联
- 体现全栈分析的系统性思维

### 前瞻性设计
- 考虑技术发展趋势对维度的影响
- 预留扩展接口和优化空间
- 设计学习和演进机制

---

**输入格式说明：**

### 方式一：基于知识库生成
\`\`\`
知识库内容：[用户提供的运维文档、故障案例、最佳实践等]
生成需求：[希望生成什么类型的分析维度，或者优化哪个现有维度]
\`\`\`

### 方式二：基于场景生成
\`\`\`
技术场景：[具体的技术栈、架构模式、问题类型]
分析需求：[希望解决什么类型的运维问题]
\`\`\`

### 方式三：混合输入
\`\`\`
知识库内容：[用户的实际知识库]
技术场景：[补充的技术背景信息]
优化目标：[具体的改进需求]
\`\`\`

现在请基于以下输入信息设计相应的分析维度：

[在此处输入知识库内容、技术场景或优化需求]`,je=g({__name:`Index`,setup(t,{expose:n}){let{locale:u}=(0,re.useI18n)(),d=x(``),p=x(!1),g=x(!1),_=x(!1),v=x(null),y=x(null),b=x(!1),S=x([]),C=x(!1),{copy:w}=B({legacy:!0}),T=async()=>{try{await w(Ae),L.success(V(`提示词已复制到剪贴板`))}catch{L.error(V(`复制失败`))}},D=x([{key:`introduction`,title:V(`分析维度体系介绍`),content:`# 分析维度体系介绍

## 什么是分析维度

**分析维度**是 DeepFlow 智能体进行根因分析的基本分析单元，每个维度代表一种特定的分析视角和方法论。它将复杂的系统故障问题分解为多个可独立分析的专业领域，使智能体能够像领域专家一样，从不同角度系统性地诊断问题。

## 核心设计理念

### 专业化分工
每个分析维度专注于特定类型的问题和分析方法：
- **领域专精**：深入某一技术领域的分析能力
- **方法独立**：拥有完整的分析步骤和判断标准
- **工具集成**：整合该领域最佳的分析工具和技巧

### 模块化架构
分析维度采用模块化设计，支持：
- **动态扩展**：可以根据业务需要不断新增维度
- **独立演进**：每个维度可以独立优化和升级
- **灵活组合**：根据问题特征选择合适的维度组合

### 知识驱动
每个维度通过知识库持续学习和增强：
- **经验积累**：从历史案例中提取分析方法
- **标准细化**：不断完善判断标准和阈值
- **技巧传承**：将专家经验转化为可执行的分析步骤

## 分析维度的构成要素

### 1. 分析目标 (Target)
明确该维度要解决什么类型的问题
- 适用的故障场景和问题类型
- 分析的重点关注领域
- 预期达成的诊断目标

### 2. 分析方法 (Method)
定义具体的分析步骤和流程
- 标准化的分析步骤序列
- 关键检查点和判断标准
- 多路径分析的决策逻辑

### 3. 工具集 (Tools)
集成该维度所需的分析工具
- 数据采集和分析工具
- 监控指标和日志分析
- 专业的诊断和追踪工具

### 4. 输出标准 (Output)
规范分析结果的格式和内容
- 结构化的分析结论
- 量化的影响评估
- 可执行的修复建议

### 5. 知识增强 (Enhancement)
持续学习和优化的机制
- 从知识库提取新的分析方法
- 细化判断标准和阈值
- 积累领域专家经验

## 维度体系的动态特性

### 持续扩展
分析维度不是固定的，会根据以下因素持续增加：

**技术发展驱动**
- 新技术栈的引入（如容器、微服务、云原生）
- 新的监控和分析工具的出现
- 架构模式的演进带来新的故障类型

**业务需求驱动**
- 特定行业的专业分析需求
- 公司特有的技术架构和问题模式
- 监管和合规要求的特殊分析维度

**经验积累驱动**
- 从复杂故障案例中发现新的分析角度
- 用户反馈中识别的分析盲区
- 专家知识的持续输入和转化

### 智能演进
每个维度都在持续优化：
- **方法精进**：分析步骤更加精确和高效
- **标准细化**：判断阈值更加准确和适用
- **工具升级**：集成更先进的分析工具
- **经验丰富**：积累更多的实战技巧

## 维度协同工作机制

### 智能选择
智能体根据故障特征，自动选择最相关的分析维度：
- **问题类型匹配**：根据故障现象选择主要维度
- **复杂度评估**：确定需要多少个维度参与分析
- **优先级排序**：安排维度分析的先后顺序

### 并行分析
多个维度可以同时工作：
- **独立执行**：各维度并行进行分析
- **结果汇聚**：综合各维度的分析结论
- **交叉验证**：通过多维度结果验证准确性

### 迭代深化
根据初步分析结果，动态调整分析策略：
- **维度切换**：根据发现的线索切换到更合适的维度
- **深度递进**：从概览分析深入到细节分析
- **范围扩展**：根据需要引入更多相关维度

## 价值与优势

### 专业化分析
每个维度都具备特定领域的专业分析能力，避免了t('万金油')式的浅层分析

### 全面性覆盖
通过多维度组合，可以覆盖运维中遇到的各种复杂问题场景

### 可扩展性
模块化设计支持根据实际需要灵活扩展新的分析能力

### 知识传承
将专家的分析经验和方法论固化为可复用的分析维度

### 持续优化
通过知识库学习机制，分析能力可以持续改进和升级

## 应用实践

### 单维度分析
对于明确的问题类型，使用对应的专业维度进行深度分析

### 多维度协同
对于复杂问题，组合使用多个维度，从不同角度进行全面诊断

### 渐进式分析
从宏观维度开始，根据发现的线索逐步深入到更具体的维度

### 动态调整
根据分析过程中的发现，灵活调整参与分析的维度组合

---

分析维度体系是运维智能体的核心能力框架，它将复杂的运维诊断工作转化为结构化、专业化、可扩展的分析过程，既保证了分析的深度和专业性，又提供了面对新问题时的灵活性和扩展性。`},{key:`how-to-generate`,title:V(`如何生成更多分析维度`),content:`# 分析维度的两种生成方式

## 分析维度的构建主要有两种核心方式：

### 方式一：人工经验积累生成

基于资深运维专家的实战经验和领域知识进行维度设计。专家团队通过总结多年的故障处理经验，梳理不同类型问题的分析思路和方法论，将其结构化为标准的分析维度。这种方式的优势在于实战性强、针对性准确，每个维度都经过了真实场景的验证，具有很高的可靠性。但局限性在于扩展速度较慢，且容易受限于专家的知识边界和认知局限。

### 方式二：大模型智能生成

利用精心设计的<span class="bg-fill-2 px-2 h-4 w-[74px] rounded"><span class="text-title-1">提示词</span>&nbsp;&nbsp;&nbsp;&nbsp;</span>，引导顶级大模型（如 DeepSeek、Claude、GPT 等）基于海量的技术知识和案例分析来生成分析维度。通过输入具体的故障案例、技术文档或最佳实践，大模型能够快速识别分析模式、提取方法论、并构建新的维度框架。这种方式的优势在于生成速度快、覆盖面广，能够快速适应新技术栈和新问题类型，并且可以从全球范围的技术知识中汲取洞察。但需要通过精确的提示词设计和人工验证来确保生成质量。

最佳实践是将两种方式相结合：用专家经验构建核心维度基础，用大模型快速扩展和优化维度能力，形成一个既稳定可靠又灵活高效的维度生成体系。`}]),O=async()=>{C.value=!0;try{S.value=e(await z(`/ai/task-dimensions`,{method:`GET`}),t=>({...t,extend_tools:e(t.tools,e=>e.name)}))}catch{S.value=[]}finally{C.value=!1}},A=e=>{v.value={...e},p.value=!0},j=()=>{O()},M=()=>{O()},N=()=>{v.value=null,p.value=!0},P=e=>{I.confirm({title:V(`删除`),width:464,titleAlign:`start`,modalClass:`delete-confirm-modal`,content:V(`确定要删除这条分析维度吗？`),onOk:async()=>{b.value=!0;try{await z(`/ai/task-dimensions/delete`,{method:`DELETE`,urlQuery:{uuid:e.uuid}}),L.success(V(`删除成功`)),O()}catch{L.error(V(`删除失败`))}finally{b.value=!1}},okButtonProps:{type:`primary`,status:`danger`},okText:V(`确认删除`),cancelText:V(`取消`)})},F=e=>{y.value=e,g.value=!0},R=[{headerName:V(`名称`),field:`name`,sortable:!1,onCellClicked:e=>e.data&&F(e.data),cellClass:`cursor-pointer text-primary-6 font-medium`},{headerName:V(`维度描述`),field:`description`,sortable:!1,flex:1,cellRenderer:e=>`<span class="text-title-2" style="line-height: 1.5; word-break: break-word;">${e.value}</span>`},{headerName:V(`类型`),field:`type`,sortable:!1,cellRenderer:e=>`<ATag size="small" class="text-title-1 bg-fill-2 p-1 font-medium">${V(e.value)}</ATag>`},{headerName:V(`输出文案`),field:`output`,sortable:!1,cellRenderer:e=>`<span class="text-title-2" style="line-height: 1.5; word-break: break-word;">${e.value}</span>`},{headerName:V(`调用工具`),field:`tools`,sortable:!1,cellRenderer:`ToolsCellRenderer`,tooltipField:`extend_tools`,tooltipComponentParams:{showTags:!0}},{headerName:V(`更新时间`),field:`update_time`,sortable:!0,cellRenderer:e=>e.data.type===V(`系统`)?`--`:`<span class="text-title-2">${te(e.value)}</span>`},{headerName:V(`操作`),sortable:!1,pinned:`right`,width:u.value===`en-US`?105:80,maxWidth:u.value===`en-US`?105:80,cellRenderer:`OperationCellRenderer`,cellRendererParams:{onEdit:A,onDelete:P}}];return o(()=>{O()}),n({OperationCellRenderer:Z,ToolsCellRenderer:$}),(e,t)=>{let n=E,o=r(`YsIcon`),u=k,b=r(`YsAgTable`);return f(),i(`div`,Se,[m(`div`,Ce,[m(`div`,we,[l(n,{modelValue:d.value,"onUpdate:modelValue":t[0]||=e=>d.value=e,placeholder:c(V)(`搜索`),class:`w-64`},null,8,[`modelValue`,`placeholder`]),m(`div`,Te,[l(o,{icon:`common-hints-question`,class:`group-hover:text-primary-6`}),m(`span`,{class:`group-hover:text-primary-6 cursor-pointer`,onClick:t[1]||=e=>_.value=!0},s(c(V)(`生成更多分析维度`)),1)])]),m(`div`,Ee,[l(u,{type:`primary`,size:`medium`,onClick:N},{icon:h(()=>[l(o,{icon:`ai-plus`})]),default:h(()=>[a(` `+s(c(V)(`新增维度`)),1)]),_:1})])]),m(`div`,De,[m(`div`,Oe,[m(`div`,ke,[l(b,{ref:`tableRef`,columns:R,data:S.value,loading:C.value,pagination:!0,search:d.value,"page-size":20,component:{OperationCellRenderer:Z,ToolsCellRenderer:$}},null,8,[`data`,`loading`,`search`,`component`])])])]),l(G,{visible:p.value,"onUpdate:visible":t[2]||=e=>p.value=e,record:v.value,onAdd:j,onEdit:M},null,8,[`visible`,`record`]),l(ne,{visible:_.value,"onUpdate:visible":t[3]||=e=>_.value=e,tabs:D.value,"default-active-tab":`introduction`,"copy-prompt-content":T},null,8,[`visible`,`tabs`]),l(ye,{visible:g.value,"onUpdate:visible":t[4]||=e=>g.value=e,record:y.value},null,8,[`visible`,`record`])])}}});export{je as default};