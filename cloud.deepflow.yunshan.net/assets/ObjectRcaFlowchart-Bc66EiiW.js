import{s as e}from"./rolldown-runtime-C5c2KzVm.js";import{Ct as t,d as n,xt as r}from"./d3-vendor-Hbl8Isc-.js";import{C as i}from"./utils-vendor-1-BVqXzy4r.js";import{$ as a,B as o,C as s,D as c,It as l,K as u,Lt as d,Nt as f,O as p,Pt as m,S as h,St as g,U as _,Y as v,b as y,dt as b,k as x,n as S,st as C,wt as w,x as T,y as E}from"./@vue-vendor-BiAdlnhr.js";import{Mn as D,Tn as O,an as k,g as A,in as j,zt as M}from"./vue-vendor-Ri77IBGf.js";import{a as N}from"./datetime-vendor-DytgT_Kc.js";import{a as P}from"./i18n-hpZHBxA1.js";import{t as F}from"./_plugin-vue_export-helper-CRt-r6Cj.js";import{$t as I,Et as ee,Ft as L,Ht as R,It as te,Lt as ne,Mt as re,Pt as z,Qt as B,Tt as V,Ut as H,Xt as U,Yt as W,Zt as G,qt as K,wt as q}from"./Line-CCNc7Ohr.js";import{a as J}from"./tools-BWvMSDSg.js";import{n as Y}from"./parser-vendor-jbtSt_7W.js";S();var ie={class:`bg-fill-1 mb-3 rounded-lg p-3`},ae={class:`results-header flex items-center gap-2`},oe={class:`mermaid-container`},se={key:1,class:`empty-content`},X=F(x({__name:`Mermaid`,props:{mermaidDiagram:{}},setup(e){let t=e,n=E(()=>t.mermaidDiagram?`\`\`\`mermaid
${t.mermaidDiagram}
\`\`\``:``);return(e,t)=>{let r=a(`YsIcon`),i=a(`MdPreview`);return v(),s(`div`,ie,[y(`div`,ae,[p(r,{icon:`ai-thinking`}),y(`span`,null,d(f(P)(`分析思路1`)),1)]),y(`div`,oe,[n.value?(v(),T(i,{key:0,"model-value":n.value,toolbars:[],class:`mermaid-content`},null,8,[`model-value`])):(v(),s(`div`,se,d(f(P)(`暂无分析思路图表`)),1))])])}}}),[[`__scopeId`,`data-v-85e3b012`]]),Z=e(n()),ce=`var(--fill-2)`;function le(e,t=150,n=50,r=`TB`){try{let i=Y(e);if(!i||!i.flowchart)return J.error(`无效的 YAML 结构：未找到 flowchart 数组。`),null;let a=new Z.graphlib.Graph({compound:!0,multigraph:!1});a.setGraph({rankdir:r,nodesep:70,ranksep:70,marginx:20,marginy:20}),a.setDefaultEdgeLabel(()=>({}));let o=i.flowchart,s={},c={},l={};o.forEach(e=>{if(!e.id||!e.name){J.warn(`跳过节点，因为缺少 id 或 name:`,e);return}a.setNode(e.id,{label:e.name,status:e.status,result:e.result,width:t,height:n}),l[e.id]={status:e.status,result:e.result},e.group&&(s[e.group]||(s[e.group]={label:e.group}),c[e.id]=e.group)}),Object.keys(s).forEach(e=>{a.setNode(e,{label:s[e].label,clusterLabelPos:`top`})}),Object.keys(c).forEach(e=>{let t=c[e];a.hasNode(e)&&a.hasNode(t)?a.setParent(e,t):J.warn(`无法设置父节点：节点 '${e}' 或 组 '${t}' 在图中未找到。`)}),o.forEach(e=>{if(!a.hasNode(e.id))return;let t=e.id;e.next&&(Array.isArray(e.next)?e.next:[e.next]).forEach(e=>{a.hasNode(e)?a.setEdge(t,e):J.warn(`边的目标节点 '${e}' 未找到 (源节点 '${t}')。`)}),e.branches&&e.branches.forEach(e=>{let n=e.next;a.hasNode(n)?a.setEdge(t,n,{label:e.if}):J.warn(`分支的目标节点 '${n}' 未找到 (源节点 '${t}')。`)})}),Z.layout(a);let u=[],d=[];a.nodes().forEach(e=>{let t=a.node(e);t&&(s[e]?d.push({id:e,label:t.label||``,x:t.x,y:t.y,width:t.width,height:t.height}):u.push({id:e,label:t.label||``,x:t.x,y:t.y,width:t.width,height:t.height,group:c[e]||void 0,status:l[e]?.status,result:l[e]?.result}))});let f=a.edges().map(e=>{let t=a.edge(e);return{v:e.v,w:e.w,points:t.points,label:t.label}}),p=a.graph();return{nodes:u,edges:f,groups:d,graph:{width:p.width??0,height:p.height??0}}}catch(e){return J.error(`处理流程图 YAML 或计算布局时出错:`,e),null}}function ue(e,n,a={animate:!0}){let o=t(e).select(`svg.flowchart`);o.empty()&&(o=W(e).classed(`flowchart`,!0),U(o));let s=o.select(`g.container`);s.empty()&&(s=o.append(`g`).attr(`class`,`container`));let c=s.select(`g.group`);c.empty()&&(c=s.append(`g`).attr(`class`,`group`));let l=s.select(`g.node`);l.empty()&&(l=s.append(`g`).attr(`class`,`node`));let u=s.select(`g.link`);u.empty()&&(u=s.append(`g`).attr(`class`,`link`));let d=n.nodes.map(e=>{let t=new G(l,new I({x:e.x-e.width/2,y:e.y-e.height/2,w:e.width,h:e.height}));return t.data=e,t.setRenderFunc(H),t}),f;a.animate&&(f=s.transition().duration(500));let p={发现问题:V,一切正常:z},m={发现问题:ee,一切正常:L},h={发现问题:V,一切正常:z},g={getNodeTitle:e=>e.data.label,getNodeStroke:e=>p[e.data.status]||`var(--color-border-3)`,getNodeFill:e=>m[e.data.status]||ce,getNodeTextColor:e=>h[e.data.status]||`var(--color-text-1)`,transition:f},_=i(l.selectAll(`g.node`).data(),e=>e.data.id),v=[];l.selectAll(`g.node`).data(d,e=>e.data.id).join(e=>{let t=e.data();return v=t,r(t.map(e=>(e.setRenderFunc(H),e.render({...g,transition:null}),e.refs.ele.datum(e),e.refs.ele)).map(e=>e.node()))},e=>(e.data().forEach(e=>{let t=_[e.data.id];e.refs={...t.refs},e.props.oldRect=new I({x:t.rect.x,y:t.rect.y,h:t.rect.h,w:t.rect.w}),e.setRenderFunc(H),e.render(g)}),e),e=>{e.remove()}),l.selectAll(`g.node`).each(function(e){let n=typeof e?.data?.label==`string`?e.data.label.trim():``,r=typeof e?.data?.result==`string`?e.data.result.trim():``,i=typeof e?.data?.status==`string`?e.data.status.trim():``,a=[n,r,i?`状态：${i}`:``].filter(Boolean).join(`
`),o=t(this),s=o.select(`title`);if(s.empty()){o.append(`title`).text(a||n||`节点`);return}s.text(a||n||`节点`)});let y=n.edges.map(e=>{let t=new B(u);return t.data=e,t.from=d.find(t=>t.data.id===e.v),t.to=d.find(t=>t.data.id===e.w),t.props.hash=`${e.v}-${e.w}`,t.setRenderFunc(K),t}),b={svg:s,getLinkColor:()=>re,getLinkV:()=>1,getLinkSize:()=>2,markLinkArrow:!0,linkVertical:!0,transition:f},x=i(u.selectAll(`g.link`).data(),e=>e.props.hash);u.selectAll(`g.link`).data(y,e=>e.props.hash).join(e=>r(e.data().map(e=>(e.setRenderFunc(K),e.render(b),e.refs.ele.datum(e),e.refs.ele)).map(e=>e.node())),e=>(e.data().forEach(e=>{e.refs={...x[e.props.hash].refs},e.setRenderFunc(K),e.render(b)}),e),e=>{e.remove()});let S=n.groups.map(e=>{let t=new G(c,new I({x:e.x-e.width/2,y:e.y-e.height/2,w:e.width,h:e.height}));return t.data=e,t.setRenderFunc(R),t}),C=i(c.selectAll(`g.group`).data(),e=>e.data.id),w={getGroupTitle:e=>e.data.label,getGroupFill:()=>q,getGroupTitleColor:()=>te};c.selectAll(`g.group`).data(S,e=>e.data.id).join(e=>r(e.data().map(e=>(e.setRenderFunc(R),e.render({...w,transition:null}),e.refs.ele.datum(e),e.refs.ele)).map(e=>e.node())),e=>(e.data().forEach(e=>{let t=C[e.data.id];e.refs={...t.refs},e.props.oldRect=new I({x:t.rect.x,y:t.rect.y,h:t.rect.h,w:t.rect.w}),e.setRenderFunc(R),e.render(w)}),e),e=>{e.remove()});let T={nodes:d,links:y,svg:o,container:s},E=ne(e,T,{enableDrag:!0,enableMinimap:!1});return v.length>0&&E.centerNodes(0,v),{...E,handler:T}}function de(e,t=fe){J.log(`👀 渲染yaml图表`,e);let n=le(t,150,50,`TB`);n&&ue(e,n)}var fe=`
flowchart:
  # 开始
  - id: p1
    name: 问题解析
    type: start
    next: t1
    result: "从问题中提取到业务名称：电商系统，对象类型：API，对象名称：/shop/full-test，问题现象：响应慢，时间范围：今天上午8点到8点10分（2025-04-28 08:00:00 - 2025-04-28 08:10:00）"
  - id: t1
    name: 任务分类
    type: decision
    branches:
      - next: f1
        if: 是故障定位问题
      - next: u1
        if: 是不相关问题
      - next: i1
        if: 是不合理问题
    result: "确认是故障定位问题，与系统性能或异常状态相关"
  - id: f1
    name: 故障识别
    type: decision
    branches:
      - next: b1
        if: 是时延瓶颈问题
      - next: a1
        if: 是异常问题
    result: "确认是时延瓶颈问题，特征为API响应慢，无明显错误或异常"
  - id: b1
    name: 时延瓶颈问题确认
    next: b2
    result: "确认电商系统 /shop/full-test API 存在响应慢的瓶颈问题，初步判断API性能可能存在问题或资源瓶颈"
  - id: b2
    name: 使用黄金指标定位时延瓶颈时间范围
    type: tool
    next: b3
    result: "通过黄金指标工具分析，瓶颈主要发生在08:09:00到08:10:00期间，平均响应时延29.44秒"
    status: "发现问题"
  - id: b3
    name: 使用业务拓扑追踪时延瓶颈
    type: tool
    next:
      - t2
      - t3
      - t4
      - t5
      - t6
    result: "通过业务拓扑追踪发现主要瓶颈点在pod_cluster=T0-Sandbox访问pod_service=ingress-nginx-controller-admission,region=阿里云-BJ，平均响应时间29.44秒"
    status: "发现问题"
  # 错误码分析 (已完成)
  - id: t2
    name: 进入错误码分析维度
    group: 错误码分析
    type: decision
    next:
      - t2_1
      - t2_2
      - t2_3
      - t2_4
      - t2_5
    result: "决定对 /shop/full-test API 进行错误码分析，检查是否存在由于错误码引起的性能问题"
  - id: t2_1
    name: 分析调用日志
    type: tool
    group: 错误码分析
    next: t2_out
    result: "已执行。发现大量HTTP 502 Bad Gateway错误和404 Not Found错误，这可能是导致响应慢的主要原因之一。"
    status: "发现问题"
  - id: t2_2
    name: 分析网络TCP连接错误信息
    type: tool
    group: 错误码分析
    next: t2_out
    result: "已执行。发现了较高的重传比例，这可能是导致响应慢的一个因素。"
    status: "发现问题"
  - id: t2_3
    name: 分析应用日志
    type: tool
    group: 错误码分析
    next: t2_out
    result: "已执行。没有查询到相关数据。"
    status: "无数据"
  - id: t2_4
    name: 分析网络异常类型
    type: tool
    group: 错误码分析
    next: t2_out
    result: "已执行。没有查询到相关数据。"
    status: "无数据"
  - id: t2_5
    name: 分析资源变更事件
    type: tool
    group: 错误码分析
    next: t2_out
    result: "已执行。没有查询到相关数据。"
    status: "无数据"
  - id: t2_out
    name: 错误码分析结果汇总
    group: 错误码分析
    type: decision
    result: "错误码分析完成。发现大量HTTP 502 Bad Gateway错误和404 Not Found错误，这可能是导致响应慢的主要原因之一。此外，网络TCP连接错误信息也显示了较高的重传比例。排除了网络异常类型和资源变更事件的影响。"
  # 资源变更影响分析 (已完成)
  - id: t3
    name: 进入资源变更影响分析维度
    group: 资源变更影响分析
    type: decision
    next:
      - t3_1
      - t3_2
    result: "决定分析资源变更是否对系统性能产生了影响"
  - id: t3_1
    name: 使用黄金指标定位瓶颈时间范围
    type: tool
    group: 资源变更影响分析
    next: t3_out
    result: "已执行。通过黄金指标工具分析，瓶颈主要发生在08:09:00到08:10:00期间，平均响应时延29.44秒。"
    status: "发现问题"
  - id: t3_2
    name: 分析资源变更事件
    type: tool
    group: 资源变更影响分析
    next: t3_out
    result: "已执行。没有查询到相关数据。"
    status: "无数据"
  - id: t3_out
    name: 资源变更影响分析结果汇总
    group: 资源变更影响分析
    type: decision
    result: "资源变更影响分析完成。没有发现相关资源变更事件，排除了资源变更的影响。"
  # 性能关联分析 (已完成)
  - id: t4
    name: 进入性能关联分析维度
    group: 性能关联分析
    type: decision
    next:
      - t4_1
      - t4_2
      - t4_3
      - t4_4
      - t4_5
    result: "决定对 /shop/full-test API 进行性能关联分析，将分析CPU、内存等基础资源使用情况和性能热点函数"
  - id: t4_1
    name: 分析性能剖析数据
    type: tool
    group: 性能关联分析
    next: t4_out
    result: "已执行。性能分析数据没有异常。"
    status: "一切正常"
  - id: t4_2
    name: 分析基础指标数据
    type: tool
    group: 性能关联分析
    next: t4_out
    result: "已执行。df_pod=nginx-ingress-controller-694d8bf977-h65dm, _querier_region=阿里云-BJ内存使用率：value指标在04-28 07:50到04-28 08:10期间变化不大，value指标平均值为846.85M%."
    status: "一切正常"
  - id: t4_3
    name: 分析文件读写事件
    type: tool
    group: 性能关联分析
    next: t4_out
    result: "已执行。查询到时间段内的磁盘IO事件：1. 实例: nginx-ingress-controller-694d8bf977-h65dm, 类型: pod, 事件类型: 读, Avg(持续时间): 14us."
    status: "一切正常"
  - id: t4_4
    name: 分析调用日志
    type: tool
    group: 性能关联分析
    next: t4_out
    result: "已执行。发现大量的HTTP 502 Bad Gateway错误和404 Not Found错误。"
    status: "发现问题"
  - id: t4_5
    name: 分析网络性能指标数据
    type: tool
    group: 性能关联分析
    next: t4_out
    result: "已执行。没有查到数据。"
    status: "无数据"
  - id: t4_out
    name: 性能关联分析结果汇总
    group: 性能关联分析
    type: decision
    result: "性能关联分析完成。发现大量的HTTP 502 Bad Gateway错误和404 Not Found错误。文件读写事件正常。网络性能指标数据未查询到。"
  # 外部依赖分析 (未完成，只显示入口节点)
  - id: t5
    name: 进入外部依赖分析维度
    group: 外部依赖分析
    type: decision
    next: t5_out
    result: "决定分析 /shop/full-test API 与下游服务的交互情况，检查是否存在依赖服务问题"
  - id: t5_out
    name: 外部依赖分析结果汇总
    group: 外部依赖分析
    type: decision
    next: c1
    result: "外部依赖分析未完成。未能收集足够的数据进行分析。"
  # 网络传输与路径分析 (已完成)
  - id: t6
    name: 进入网络传输与路径分析维度
    group: 网络传输与路径分析
    type: decision
    next:
      - t6_1
      - t6_2
      - t6_3
    result: "决定分析服务间的数据传输质量和网络路径状况"
  - id: t6_1
    name: 使用全栈路径追踪时延瓶颈
    type: tool
    group: 网络传输与路径分析
    next: t6_out
    result: "已执行。路径: pod_cluster=T0-Sandbox -> pod_service=ingress-nginx-controller-admission应用全栈链路：总时延为 1.62s，时延瓶颈点在 服务端进程(采集网卡：13152) 到 服务端进程(采集网卡：13150) 之间，时延差值为 1.6s。网络全栈路径：整个网络链路中，checkoutservice-7fbd49c944-tr9pc(客户端容器节点) 请求 nginx-ingress-controller-694d8bf977-h65dm(服务端网卡) 时有最大TCP的建连时延差值: 952.88ms(--- > 952.88ms)"
    status: "发现问题"
  - id: t6_2
    name: 分析网络性能指标数据
    type: tool
    group: 网络传输与路径分析
    next: t6_out
    result: "已执行。零窗指标都为0包重传比例指标在如下时间段较高：08:07:00到08:10:00，重传比例指标平均值为11.27%."
    status: "发现问题"
  - id: t6_3
    name: 分析应用超时比例
    type: tool
    group: 网络传输与路径分析
    next: t6_out
    result: "尚未执行。"
    status: "工具异常"
  - id: t6_out
    name: 网络传输与路径分析结果汇总
    group: 网络传输与路径分析
    type: decision
    result: "网络传输与路径分析完成。发现高重传比例和较大的TCP建连时延。排除了应用超时比例的影响。"
  # 综合判断分析
  - id: c1
    name: 综合判断分析
    type: decision
    result: "综合判断分析完成。发现大量HTTP 502 Bad Gateway错误和404 Not Found错误，这可能是导致响应慢的主要原因之一。此外，网络传输与路径分析显示了较高的重传比例和较大的TCP建连时延。排除了资源变更事件和网络异常类型的影响。"
`;S();var pe={class:`bg-fill-1 mb-3 rounded-lg p-3`},me={class:`results-header flex items-center gap-2`},he={key:0,class:`absolute top-2 right-2 z-51`},ge={key:0,class:`floating-header`},_e={class:`suspension-title`},Q={class:`absolute top-1 right-2 z-51`},$=F(x({__name:`ThinkingChain`,props:{mermaidDiagram:{},enableFloating:{type:Boolean,default:!0}},emits:[`update:enableFloating`],setup(e){let t=e,n=w(null),r=`flowchart-${N()}`,i=w(t.enableFloating),c=null,x=()=>{i.value=!i.value,i.value||(E.visible=!0)};u(()=>{S(),c=document.querySelector(`.chat-history-container`),c?.addEventListener(`scroll`,O)}),C(()=>t.mermaidDiagram,()=>{S()}),C(()=>t.enableFloating,()=>{i.value=t.enableFloating});let S=async()=>{let e=t.mermaidDiagram;e&&de(`#${r}`,e)};function T(e,t){return!e||!t?!0:e.getBoundingClientRect().top-t.getBoundingClientRect().top>=0}let E=g({visible:!0,contentHeight:0}),O=()=>{if(!i.value){E.visible=!0;return}let e=T(n.value,c),t=e!==E.visible;E.visible=e,E.visible&&(E.contentHeight=n.value?.clientHeight??0),t&&o(()=>{})};return _(()=>{c?.removeEventListener(`scroll`,O),c=null}),(t,o)=>{let c=a(`YsIcon`),u=D,g=M;return v(),s(`div`,pe,[y(`div`,me,[p(c,{icon:`ai-thinking`}),y(`span`,null,d(f(P)(`分析思路`)),1)]),y(`div`,{ref_key:`mermaidContainerRef`,ref:n,class:`mermaid-container relative`,style:l({minHeight:`${E.contentHeight}px`,border:`none`})},[E.visible?(v(),s(`div`,he,[p(g,{content:i.value?f(P)(`取消固定`):f(P)(`固定`)},{default:b(()=>[p(u,{type:`text`,size:`mini`,class:`bg-(--color-bg-2)! hover:bg-(--color-fill-2)!`,onClick:x},{default:b(()=>[p(f(A),{"stroke-width":i.value?5:1,style:l({transform:i.value?`rotate(-45deg)`:`none`,color:i.value?`var(--color-primary)`:`var(--color-text-2)`})},null,8,[`stroke-width`,`style`])]),_:1})]),_:1},8,[`content`])])):h(``,!0),y(`div`,{id:r,class:m([`mermaid-content top-[40px] right-[40px] h-[440px] p-0!`,{"border-border-1 fixed z-50 h-[284px]! w-[550px] overflow-hidden rounded-md border shadow-[0px_0px_20px_0px_rgba(0,0,0,0.06)]":!E.visible}]),style:l({display:e.mermaidDiagram?`block`:`none`})},[E.visible?h(``,!0):(v(),s(`div`,ge,[y(`div`,_e,d(f(P)(`思维链`)),1),y(`div`,Q,[p(g,{content:i.value?f(P)(`取消固定`):f(P)(`固定`)},{default:b(()=>[p(u,{type:`text`,size:`mini`,class:`bg-(--color-bg-2)! hover:bg-(--color-fill-2)!`,onClick:x},{default:b(()=>[p(f(A),{"stroke-width":i.value?5:1,style:l({transform:i.value?`rotate(-45deg)`:`none`,color:i.value?`var(--color-primary)`:`var(--color-text-2)`})},null,8,[`stroke-width`,`style`])]),_:1})]),_:1},8,[`content`])])]))],6)],4)])}}}),[[`__scopeId`,`data-v-6e158e41`]]);S();var ve={class:`space-y-2`},ye={class:`flex items-center justify-end`},be=x({__name:`ObjectRcaFlowchart`,props:{flowchartYaml:{},mermaidDiagram:{}},setup(e){let t=e,n=e=>typeof e==`string`?e.trim():``,r=e=>e.replace(/"/g,`'`).replace(/[\r\n]+/g,` `).replace(/\s+/g,` `).trim(),i=e=>{let t=e.replace(/[^a-zA-Z0-9_]/g,`_`);return t.length>0?t:`n_${Math.random().toString(36).slice(2,8)}`},a=e=>/^(flowchart|graph|sequenceDiagram|classDiagram|stateDiagram|erDiagram|journey|gantt)\b/.test(e.trim()),o=e=>{if(!e.trim())return!1;try{let t=Y(e);return Array.isArray(t?.flowchart)&&t.flowchart.length>0}catch{return!1}},l=e=>{if(!e.trim())return``;try{let t=Y(e),a=Array.isArray(t?.flowchart)?t.flowchart:[];if(a.length===0)return``;let o=[`flowchart TB`],s=new Map;return a.forEach((e,t)=>{let a=n(e.id)||`node_${t+1}`,c=i(a);s.set(a,c);let l=r(n(e.name)||a);o.push(`${c}["${l}"]`)}),a.forEach(e=>{let t=n(e.id),a=t?s.get(t):void 0;if(a){if(typeof e.next==`string`){let t=s.get(e.next)??i(e.next);o.push(`${a} --> ${t}`)}else Array.isArray(e.next)&&e.next.forEach(e=>{if(typeof e!=`string`)return;let t=s.get(e)??i(e);o.push(`${a} --> ${t}`)});Array.isArray(e.branches)&&e.branches.forEach(e=>{let t=n(e.next);if(!t)return;let c=s.get(t)??i(t),l=r(n(e.if));l?o.push(`${a} -->|${l}| ${c}`):o.push(`${a} --> ${c}`)})}}),o.join(`
`)}catch{return``}},u=E(()=>{let e=n(t.flowchartYaml);if(o(e))return e;let r=n(t.mermaidDiagram);return o(r)?r:``}),d=E(()=>{let e=n(t.mermaidDiagram);if(a(e))return e;let r=n(t.flowchartYaml);return a(r)?r:l(u.value)}),f=E(()=>o(u.value)),m=E(()=>d.value.length>0),h=w(`mermaid`);return C([f,m],([e,t])=>{if(t){h.value=`mermaid`;return}if(e){h.value=`diagram`;return}h.value=`diagram`},{immediate:!0}),(e,t)=>{let n=j,r=k,i=O;return v(),s(`div`,ve,[y(`div`,ye,[p(r,{modelValue:h.value,"onUpdate:modelValue":t[0]||=e=>h.value=e,type:`button`,size:`mini`},{default:b(()=>[p(n,{value:`diagram`,disabled:!f.value},{default:b(()=>[...t[1]||=[c(`Diagram`,-1)]]),_:1},8,[`disabled`]),p(n,{value:`mermaid`,disabled:!m.value},{default:b(()=>[...t[2]||=[c(`Mermaid`,-1)]]),_:1},8,[`disabled`])]),_:1},8,[`modelValue`])]),h.value===`diagram`&&f.value?(v(),T($,{key:0,"mermaid-diagram":u.value,"enable-floating":!1},null,8,[`mermaid-diagram`])):h.value===`mermaid`&&m.value?(v(),T(X,{key:1,"mermaid-diagram":d.value},null,8,[`mermaid-diagram`])):(v(),T(i,{key:2,description:`暂无可展示流程图`}))])}}});export{$ as n,X as r,be as t};