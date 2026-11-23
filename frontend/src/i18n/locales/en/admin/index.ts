import proxyGroups from './proxyGroups'
import overview from './overview'
import channels from './channels'
import accounts from './accounts'
import models from './models'
import resources from './resources'
import ops from './ops'
import settings from './settings'
import audit from './audit'
import promptAudit from './promptAudit'
import plugins from './plugins'

export default {
  ...proxyGroups,
  ...overview,
  ...channels,
  ...accounts,
  ...models,
  ...resources,
  ...ops,
  ...settings,
  ...audit,
  ...promptAudit,
  ...plugins,
}
