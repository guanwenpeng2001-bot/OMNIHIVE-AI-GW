// Local provider artwork; provenance and license notices are in LICENSES.txt.
// Monograms are explicit text fallbacks, never fabricated brand logos.
import a6apiUrl from './a6api.png?url'
import aicodemirrorUrl from './aicodemirror.svg?url'
import aicodewithUrl from './aicodewith.svg?url'
import aicodingUrl from './aicoding.svg?url'
import aigocodeUrl from './aigocode.svg?url'
import aihubmixUrl from './aihubmix.svg?url'
import amuxUrl from './amux.svg?url'
import apikey_funUrl from './apikey_fun.png?url'
import apinebulaUrl from './apinebula.png?url'
import astron_codingUrl from './astron_coding.svg?url'
import atlascloudUrl from './atlascloud.png?url'
import azure_openaiUrl from './azure_openai.svg?url'
import bailingUrl from './bailing.ico?url'
import byteplusUrl from './byteplus.png?url'
import ccsubUrl from './ccsub.svg?url'
import cherryinUrl from './cherryin.png?url'
import claudeapiUrl from './claudeapi.png?url'
import claudecnUrl from './claudecn.png?url'
import code0Url from './code0.png?url'
import code_9527Url from './code_9527.svg?url'
import compshareUrl from './compshare.svg?url'
import crazyrouterUrl from './crazyrouter.svg?url'
import cubenceUrl from './cubence.svg?url'
import dmxapiUrl from './dmxapi.png?url'
import eflowcodeUrl from './eflowcode.png?url'
import fennoaiUrl from './fennoai.webp?url'
import fluxaUrl from './fluxa.png?url'
import github_copilotUrl from './github_copilot.svg?url'
import jiekouUrl from './jiekou.svg?url'
import kaggleUrl from './kaggle.svg?url'
import kat_coderUrl from './kat_coder.svg?url'
import longcatUrl from './longcat.svg?url'
import micuUrl from './micu.svg?url'
import mimoUrl from './mimo.svg?url'
import modelscopeUrl from './modelscope.svg?url'
import nous_researchUrl from './nous_research.svg?url'
import novitaUrl from './novita.svg?url'
import nvidiaUrl from './nvidia.svg?url'
import packycodeUrl from './packycode.svg?url'
import patewayUrl from './pateway.jpg?url'
import pipellmUrl from './pipellm.png?url'
import ppioUrl from './ppio.svg?url'
import qiniuUrl from './qiniu.svg?url'
import qwencloudUrl from './qwencloud.svg?url'
import relaxycodeUrl from './relaxycode.png?url'
import rightcodeUrl from './rightcode.svg?url'
import runapiUrl from './runapi.jpg?url'
import shengsuanyunUrl from './shengsuanyun.svg?url'
import siliconflow_cnUrl from './siliconflow_cn.svg?url'
import soleapiUrl from './soleapi.svg?url'
import soshowUrl from './soshow.png?url'
import sssaicodeUrl from './sssaicode.svg?url'
import stepfun_apiUrl from './stepfun_api.svg?url'
import subrouterUrl from './subrouter.svg?url'
import sudocode_chatUrl from './sudocode_chat.png?url'
import sudocode_usUrl from './sudocode_us.png?url'
import teamorouterUrl from './teamorouter.png?url'
import therouterUrl from './therouter.svg?url'
import togetherUrl from './together.svg?url'
import xycaiUrl from './xycai.png?url'
import zetaapiUrl from './zetaapi.png?url'

export type ProviderIcon =
  | { kind: 'asset'; src: string; monochrome: boolean }
  | { kind: 'monogram'; text: string }

const icons: Record<string, ProviderIcon> = {
  a6api: { kind: 'asset', src: a6apiUrl, monochrome: false },
  aicodemirror: { kind: 'asset', src: aicodemirrorUrl, monochrome: false },
  aicodewith: { kind: 'asset', src: aicodewithUrl, monochrome: true },
  aicoding: { kind: 'asset', src: aicodingUrl, monochrome: false },
  aigocode: { kind: 'asset', src: aigocodeUrl, monochrome: false },
  aihubmix: { kind: 'asset', src: aihubmixUrl, monochrome: true },
  amux: { kind: 'asset', src: amuxUrl, monochrome: true },
  apikey_fun: { kind: 'asset', src: apikey_funUrl, monochrome: false },
  apinebula: { kind: 'asset', src: apinebulaUrl, monochrome: false },
  astron_coding: { kind: 'asset', src: astron_codingUrl, monochrome: true },
  atlascloud: { kind: 'asset', src: atlascloudUrl, monochrome: false },
  azure_openai: { kind: 'asset', src: azure_openaiUrl, monochrome: true },
  bailing: { kind: 'asset', src: bailingUrl, monochrome: false },
  byteplus: { kind: 'asset', src: byteplusUrl, monochrome: false },
  ccsub: { kind: 'asset', src: ccsubUrl, monochrome: false },
  cherryin: { kind: 'asset', src: cherryinUrl, monochrome: false },
  claudeapi: { kind: 'asset', src: claudeapiUrl, monochrome: false },
  claudecn: { kind: 'asset', src: claudecnUrl, monochrome: false },
  code0: { kind: 'asset', src: code0Url, monochrome: false },
  code_9527: { kind: 'asset', src: code_9527Url, monochrome: true },
  compshare: { kind: 'asset', src: compshareUrl, monochrome: false },
  crazyrouter: { kind: 'asset', src: crazyrouterUrl, monochrome: true },
  cubence: { kind: 'asset', src: cubenceUrl, monochrome: false },
  dmxapi: { kind: 'asset', src: dmxapiUrl, monochrome: false },
  eflowcode: { kind: 'asset', src: eflowcodeUrl, monochrome: false },
  fennoai: { kind: 'asset', src: fennoaiUrl, monochrome: false },
  fluxa: { kind: 'asset', src: fluxaUrl, monochrome: false },
  github_copilot: { kind: 'asset', src: github_copilotUrl, monochrome: true },
  jiekou: { kind: 'asset', src: jiekouUrl, monochrome: false },
  kaggle: { kind: 'asset', src: kaggleUrl, monochrome: false },
  kat_coder: { kind: 'asset', src: kat_coderUrl, monochrome: true },
  longcat: { kind: 'asset', src: longcatUrl, monochrome: true },
  micu: { kind: 'asset', src: micuUrl, monochrome: false },
  mimo: { kind: 'asset', src: mimoUrl, monochrome: true },
  modelscope: { kind: 'asset', src: modelscopeUrl, monochrome: true },
  nous_research: { kind: 'asset', src: nous_researchUrl, monochrome: true },
  novita: { kind: 'asset', src: novitaUrl, monochrome: true },
  nvidia: { kind: 'asset', src: nvidiaUrl, monochrome: true },
  packycode: { kind: 'asset', src: packycodeUrl, monochrome: true },
  pateway: { kind: 'asset', src: patewayUrl, monochrome: false },
  pipellm: { kind: 'asset', src: pipellmUrl, monochrome: false },
  ppio: { kind: 'asset', src: ppioUrl, monochrome: true },
  qiniu: { kind: 'asset', src: qiniuUrl, monochrome: true },
  qwencloud: { kind: 'asset', src: qwencloudUrl, monochrome: false },
  relaxycode: { kind: 'asset', src: relaxycodeUrl, monochrome: false },
  rightcode: { kind: 'asset', src: rightcodeUrl, monochrome: false },
  runapi: { kind: 'asset', src: runapiUrl, monochrome: false },
  shengsuanyun: { kind: 'asset', src: shengsuanyunUrl, monochrome: false },
  siliconflow_cn: { kind: 'asset', src: siliconflow_cnUrl, monochrome: true },
  soleapi: { kind: 'asset', src: soleapiUrl, monochrome: true },
  soshow: { kind: 'asset', src: soshowUrl, monochrome: false },
  sssaicode: { kind: 'asset', src: sssaicodeUrl, monochrome: false },
  stepfun_api: { kind: 'asset', src: stepfun_apiUrl, monochrome: true },
  subrouter: { kind: 'asset', src: subrouterUrl, monochrome: false },
  sudocode_chat: { kind: 'asset', src: sudocode_chatUrl, monochrome: false },
  sudocode_us: { kind: 'asset', src: sudocode_usUrl, monochrome: false },
  teamorouter: { kind: 'asset', src: teamorouterUrl, monochrome: false },
  therouter: { kind: 'asset', src: therouterUrl, monochrome: true },
  together: { kind: 'asset', src: togetherUrl, monochrome: true },
  xycai: { kind: 'asset', src: xycaiUrl, monochrome: false },
  zetaapi: { kind: 'asset', src: zetaapiUrl, monochrome: false },
}

const aliases: Record<string, string> = {
  astron_token: 'astron_coding',
  mimo_token_sg: 'mimo',
  mimo_token_eu: 'mimo',
  "compshare_coding": "compshare",
  "crazyrouter_intl": "crazyrouter",
  "dmxapi_com": "dmxapi",
  "dmxapi_ssvip": "dmxapi",
  "mimo_token_cn": "mimo",
  "qwencloud_maas_token": "qwencloud",
  "qwencloud_token": "qwencloud",
  "siliconflow_global": "siliconflow_cn",
  "stepfun_api_intl": "stepfun_api",
  "stepfun_plan": "stepfun_api",
  "stepfun_plan_intl": "stepfun_api",
  "teamorouter_com": "teamorouter"
}

export function compatibleProviderIcon(id: string, label: string): ProviderIcon {
  const key = Object.prototype.hasOwnProperty.call(aliases, id) ? aliases[id]! : id
  if (Object.prototype.hasOwnProperty.call(icons, key)) return icons[key]!
  // Future registered providers still get readable initials, not a server glyph.
  return { kind: 'monogram', text: label.replace(/[^a-z0-9]/gi, '').slice(0, 2).toUpperCase() || 'API' }
}
