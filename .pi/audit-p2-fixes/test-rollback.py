import importlib.util,json,pathlib,subprocess,unittest
P=pathlib.Path(__file__).resolve().parent
spec=importlib.util.spec_from_file_location('rollback',P/'prepare-price-rollback.py');m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
# Locate the sibling checkout explicitly; nothing in it is modified.
source=P.parent.parent.parent/'main-work'/'.pi/archive'
read=lambda p:json.loads(p.read_text(encoding='utf-8-sig'))
ops=source/'audit-ops/audit-ops';merge=source/'pricing-merge/pricing-merge'
class RollbackTests(unittest.TestCase):
 def test_matches_audit_payload(self):
  current=next(r['model_pricing'] for r in read(ops/'database.json')['queries']['group_prices']['rows'] if r['id']==4)
  proposed=m.rollback(read(merge/'group-4-before.json'),read(merge/'group-4-after.json'),current)
  self.assertEqual(proposed,read(ops/'group4-rollback-merge-only.payload.json')['model_pricing'])
  self.assertEqual((len(current),len(proposed)),(172,95))
  (P/'group4-offline-verification.json').write_text(json.dumps({'source':str(ops),'current_count':172,'candidate_count':95,'matches_audit_payload':True,'executed':False},indent=2),encoding='utf8')
 def test_preserves_later_and_non_target_edits(self):
  self.assertEqual(m.rollback([{'m':'a'}],[{'m':'a'},{'m':'b'}],[{'m':'a','price':2},{'m':'b'},{'m':'c'}]),[{'m':'a','price':2},{'m':'c'}])
 def test_group13_selected_interval(self):
  before=[{'m':str(i)} for i in range(88)];after=before+[{'m':str(i)} for i in range(88,126)]
  self.assertEqual(m.rollback(before,after,after+[{'m':'later'}]),before+[{'m':'later'}])
 def test_refuses_changed_missing_duplicate_nonappend(self):
  for current in [[{'m':'a'}],[{'m':'a'},{'m':'b','price':9}],[{'m':'a'},{'m':'b'},{'m':'b'}]]:
   with self.assertRaises(ValueError):m.rollback([{'m':'a'}],[{'m':'a'},{'m':'b'}],current)
  with self.assertRaises(ValueError):m.rollback([{'m':'a'}],[{'m':'changed'}],[])
if __name__=='__main__':unittest.main()
