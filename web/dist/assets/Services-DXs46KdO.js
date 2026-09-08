import{c as t,i as y,k as b,u as v,j as a,S as g,C as f,d as j,e as N,m as k,h as d,B as l}from"./index-7MKWYQdm.js";import{u as w}from"./useMutation-S5MIk8a-.js";import{R as _}from"./refresh-cw-BiLP1d_d.js";/**
 * @license lucide-react v0.511.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const S=[["polygon",{points:"6 3 20 12 6 21 6 3",key:"1oa8hb"}]],M=t("play",S);/**
 * @license lucide-react v0.511.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const q=[["path",{d:"M18.36 6.64A9 9 0 0 1 20.77 15",key:"dxknvb"}],["path",{d:"M6.16 6.16a9 9 0 1 0 12.68 12.68",key:"1x7qb5"}],["path",{d:"M12 2v4",key:"3427ic"}],["path",{d:"m2 2 20 20",key:"1ooewy"}]],C=t("power-off",q);/**
 * @license lucide-react v0.511.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const R=[["path",{d:"M12 2v10",key:"mnfbl"}],["path",{d:"M18.4 6.6a9 9 0 1 1-12.77.04",key:"obofu9"}]],P=t("power",R);/**
 * @license lucide-react v0.511.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const A=[["path",{d:"M21 12a9 9 0 1 1-9-9c2.52 0 4.93 1 6.74 2.74L21 8",key:"1p45f6"}],["path",{d:"M21 3v5h-5",key:"1q7to0"}]],B=t("rotate-cw",A);/**
 * @license lucide-react v0.511.0 - ISC
 *
 * This source code is licensed under the ISC license.
 * See the LICENSE file in the root directory of this source tree.
 */const D=[["rect",{width:"18",height:"18",x:"3",y:"3",rx:"2",key:"afitv7"}]],I=t("square",D),$=[{id:"start",label:"Start",icon:M,variant:"success"},{id:"reload",label:"Reload",icon:_,variant:"secondary"},{id:"restart",label:"Restart",icon:B,variant:"secondary"},{id:"stop",label:"Stop",icon:I,variant:"danger"},{id:"enable",label:"Enable",icon:P,variant:"ghost"},{id:"disable",label:"Disable",icon:C,variant:"ghost"}];function Q(){const x=y(),{push:r}=b(),n=v({queryKey:["services"],queryFn:()=>d.services(),refetchInterval:8e3}),i=w({mutationFn:({engine:s,action:e})=>d.serviceAction(s,e),onSuccess:s=>{r("success",s.output?s.output:"Done"),x.invalidateQueries({queryKey:["services"]})},onError:s=>r("error",s.message)}),p=(s,e)=>a.jsxs(a.Fragment,{children:[a.jsx(l,{color:s==="active"?"green":s==="failed"?"red":"slate",children:s||"unknown"}),a.jsx(l,{color:e==="enabled"?"blue":"slate",children:e||"—"})]});return a.jsxs("div",{className:"space-y-4",children:[a.jsxs("div",{children:[a.jsx("h1",{className:"text-lg font-bold",children:"Services"}),a.jsx("p",{className:"text-xs text-muted-foreground",children:"Start / stop / reload the proxy engines. Reload validates the generated config first — a broken config never reaches the service."})]}),n.isLoading?a.jsx("div",{className:"flex justify-center py-16",children:a.jsx(g,{})}):a.jsx("div",{className:"grid grid-cols-1 gap-4 lg:grid-cols-2",children:["nginx","haproxy"].map(s=>{var o;const e=(o=n.data)==null?void 0:o[s];return a.jsxs(f,{children:[a.jsx(j,{title:s==="nginx"?"Nginx":"HAProxy",desc:(e==null?void 0:e.description)||(e!=null&&e.binary_installed?"systemd unit":"binary not installed"),right:a.jsxs("div",{className:"flex items-center gap-2",children:[a.jsx(N,{status:(e==null?void 0:e.active)==="active"?"up":(e==null?void 0:e.active)==="failed"?"down":"unknown"}),p(e==null?void 0:e.active,e==null?void 0:e.enabled_state)]})}),a.jsxs("div",{className:"space-y-3 p-5",children:[!(e!=null&&e.binary_installed)&&a.jsxs("p",{className:"rounded-lg bg-amber-50 px-3 py-2 text-xs text-amber-700 dark:bg-amber-900/20 dark:text-amber-300",children:["The ",s," binary was not found on this server — install it or fix the path in Settings."]}),a.jsx("div",{className:"flex flex-wrap gap-2",children:$.map(({id:c,label:h,icon:u,variant:m})=>a.jsxs(k,{size:"sm",variant:m,disabled:i.isPending||!(e!=null&&e.binary_installed),onClick:()=>i.mutate({engine:s,action:c}),children:[a.jsx(u,{className:"h-3.5 w-3.5"}),h]},c))}),a.jsxs("div",{className:"grid grid-cols-2 gap-x-4 gap-y-1 border-t border-border pt-3 text-2xs text-muted-foreground",children:[a.jsx("span",{children:"Unit"}),a.jsx("span",{className:"font-mono",children:(e==null?void 0:e.unit)||s}),a.jsx("span",{children:"Sub state"}),a.jsx("span",{children:(e==null?void 0:e.sub)||"—"}),a.jsx("span",{children:"Main PID"}),a.jsx("span",{className:"font-mono",children:(e==null?void 0:e.pid)||"—"})]}),(e==null?void 0:e.error)&&a.jsx("p",{className:"text-2xs text-red-500",children:e.error})]})]},s)})})]})}export{Q as default};
