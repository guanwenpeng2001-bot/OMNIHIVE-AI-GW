import capabilities from './capabilities'
import helpGuide from './helpGuide'
import landing from './landing'
import common from './common'
import dashboard from './dashboard'
import channelMonitorV2 from './channelMonitorV2'
import batchImage from './batchImage'
import admin from './admin'
import misc from './misc'
import workspaces from './workspaces'
import modelAccess from './modelAccess'
import routing from './routing'
import wp6 from './wp6'

export default {
  workspaces,
  modelAccess,
  routing,
  wp6,
  ...capabilities,
  helpGuide,
  ...landing,
  ...common,
  ...dashboard,
  ...channelMonitorV2,
  ...batchImage,
  admin,
  ...misc,
}
