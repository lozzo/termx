export type PriceMode = 'included' | 'configured' | 'contact'
export interface CatalogPlan { id:string;name:string;eyebrow:string;description:string;price:{mode:PriceMode;label:string;monthly_minor?:number;yearly_minor?:number};cta:{label:string;href:string};featured:boolean;features:string[] }
export interface Catalog { version:number;currency:string;plans:CatalogPlan[] }
export function formatPlanPrice(plan:CatalogPlan,currency:string):string{if(plan.price.mode!=="configured"||plan.price.monthly_minor===undefined)return plan.price.label;return new Intl.NumberFormat("en-US",{style:"currency",currency,maximumFractionDigits:0}).format(plan.price.monthly_minor/100)}
export function planPriceNote(plan:CatalogPlan):string{if(plan.price.mode==="configured")return "/ month";if(plan.price.mode==="included")return "No card required";return plan.id==="pro"?"Preview access":"Custom agreement"}
