# Catalog legacy adapter

`control/catalog/models.ProviderSet` is this adapter's production caller. The adapter reuses the v1 plaza, the finite gateway model visibility resolver, project resource mappings, legacy account discovery, BE-04 verification and gateway usage/ops records. It supplies typed catalog acquisitions. The modules/catalog projector and its PostgreSQL writer own public redaction and publication.

BE-07 replaces legacy display price conversion when the exact price reader is composed. BE-08 supplies effective route eligibility, and BE-27 extends acquisition with measured action verification. BE-30 can remove this adapter once those readers and the legacy plaza share the resulting catalog projection.
