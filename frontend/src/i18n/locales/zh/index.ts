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

export default {
  workspaces,
  modelAccess,
  routing,
  costLedger,
  ...capabilities,
  helpGuide,
  ...landing,
  ...common,
  ...dashboard,
  ...channelTelemetry,
  ...batchImage,
  admin,
  ...misc,
}
