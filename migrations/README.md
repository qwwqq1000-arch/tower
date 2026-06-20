Migrations use timestamped filenames `YYYYMMDDHHMMSS_name.sql` (goose) to avoid sequence collisions across parallel branches.
Never renumber an applied migration.
