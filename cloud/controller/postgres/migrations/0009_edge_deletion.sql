ALTER TABLE relay_reservations
    DROP CONSTRAINT relay_reservations_edge_id_fkey;

COMMENT ON COLUMN relay_reservations.edge_id IS
    'Edge UUID retained for historical attribution after an Edge deployment is deleted';
