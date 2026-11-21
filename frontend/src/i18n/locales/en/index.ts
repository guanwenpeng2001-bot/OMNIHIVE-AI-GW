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
import consoleWorkbench from './consoleWorkbench'
import publicPortal from './publicPortal'

export default {
  workspaces,
  modelAccess,
  routing,
  costLedger,
  membersAccess,
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
