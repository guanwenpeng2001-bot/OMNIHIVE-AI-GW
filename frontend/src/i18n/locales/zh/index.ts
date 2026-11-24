import capabilities from './capabilities'
import helpGuide from './helpGuide'
import landing from './landing'
import common from './common'
import dashboard from './dashboard'
import channelTelemetry from './channelTelemetry'
import batchImage from './batchImage'
import admin from './admin'
import misc from './misc'
import workspaces from './workspaces'
import modelAccess from './modelAccess'
import routing from './routing'
import costLedger from './costLedger'
import membersAccess from './membersAccess'
import settingsDiagnostics from './settingsDiagnostics'
import consoleWorkbench from './consoleWorkbench'
import publicPortal from './publicPortal'

import adminPlatform from './adminPlatform'
import userConsole from './userConsole'

export default {
  ...userConsole,
  adminPlatform,
  workspaces,
  modelAccess,
  routing,
  costLedger,
  membersAccess,
  settingsDiagnostics,
  ...consoleWorkbench,
  ...capabilities,
  helpGuide,
  ...landing,
  ...common,
  ...dashboard,
  ...channelTelemetry,
  ...batchImage,
  admin,
  ...misc,
  ...publicPortal,
}
