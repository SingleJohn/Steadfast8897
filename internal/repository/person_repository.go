package repository

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PersonDBTX interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

type PersonRow struct {
	ID                  string
	Name                string
	ImagePath           *string
	ImageLocked         bool
	TmdbPersonID        *int32
	Overview            *string
	ImageTag            string
	PremiereDate        *string
	ProductionYear      *int32
	ProductionLocations string
	Genres              string
	Tags                string
	Taglines            string
	ProviderIDs         string
	BackdropPath        *string
}

type PersonListOptions struct {
	Search         string
	NameStartsWith string
	UserID         string
	Filters        []string
	Limit          int64
	Offset         int64
}

type PersonMetadataUpdate struct {
	Overview            *string
	PremiereDate        *string
	ProductionYear      *int32
	ProductionLocations any
	Genres              any
	Tags                any
	Taglines            any
	ProviderIDs         any
	TmdbPersonID        *int32
}

type ActorAdminRow struct {
	ID            string
	Name          string
	HasImage      bool
	HasBackdrop   bool
	ImageLocked   bool
	HasOverview   bool
	ProviderCount int
	TagCount      int
	WorkCount     int64
	IsJunk        bool
	ImageTag      string
}

type ActorAdminFilter struct {
	Search string
	Filter string
	Sort   string
	Order  string
	Limit  int64
	Offset int64
}

const personColumns = `id::text, name, image_path, image_locked, tmdb_person_id, overview,
	EXTRACT(EPOCH FROM updated_at)::bigint::text,
	premiere_date, production_year,
	production_locations::text, genres::text, tags::text, taglines::text, provider_ids::text, backdrop_path`

const personColumnsP = `p.id::text, p.name, p.image_path, p.image_locked, p.tmdb_person_id, p.overview,
	EXTRACT(EPOCH FROM p.updated_at)::bigint::text,
	p.premiere_date, p.production_year,
	p.production_locations::text, p.genres::text, p.tags::text, p.taglines::text, p.provider_ids::text, p.backdrop_path`

const junkNameCond = `p.name ~ '[<>]' OR p.name ~ '&(lt|gt|amp|quot|apos|#[0-9]+|#x[0-9a-fA-F]+);'`
const junkNameCondBare = `(name ~ '[<>]' OR name ~ '&(lt|gt|amp|quot|apos|#[0-9]+|#x[0-9a-fA-F]+);')`
const actorAdminFrom = `FROM persons p
	LEFT JOIN (
		SELECT person_id, COUNT(*) AS cnt
		  FROM cast_members WHERE person_id IS NOT NULL GROUP BY person_id
	) w ON w.person_id = p.id`

type PersonRepository struct {
	pool *pgxpool.Pool
}

func NewPersonRepository(pool *pgxpool.Pool) *PersonRepository {
	return &PersonRepository{pool: pool}
}

func EnsurePersonsForItem(ctx context.Context, db PersonDBTX, itemID string) error {
	if _, err := db.Exec(ctx,
		`INSERT INTO persons (name)
		   SELECT DISTINCT name FROM cast_members
		    WHERE item_id = $1::uuid AND person_id IS NULL
		      AND name IS NOT NULL AND name <> ''
		 ON CONFLICT (name) DO NOTHING`,
		itemID); err != nil {
		return err
	}
	_, err := db.Exec(ctx,
		`UPDATE cast_members cm
		    SET person_id = p.id
		   FROM persons p
		  WHERE p.name = cm.name
		    AND cm.item_id = $1::uuid
		    AND cm.person_id IS NULL`,
		itemID)
	return err
}

func PropagateCastImagesToPersons(ctx context.Context, db PersonDBTX, itemID string) error {
	_, err := db.Exec(ctx,
		`UPDATE persons p
		    SET image_path = sub.image_url,
		        updated_at = NOW()
		   FROM (
		     SELECT DISTINCT ON (person_id) person_id, image_url
		       FROM cast_members
		      WHERE item_id = $1::uuid
		        AND person_id IS NOT NULL
		        AND image_url IS NOT NULL AND image_url <> ''
		      ORDER BY person_id, order_index
		   ) sub
		  WHERE p.id = sub.person_id
		    AND p.image_locked = false
		    AND (p.image_path IS NULL OR p.image_path = '')`,
		itemID)
	return err
}

func (r *PersonRepository) ListMissingImage(ctx context.Context, limit int) ([]PersonRow, error) {
	sql := `SELECT id::text, name FROM persons
	         WHERE image_locked = false
	           AND (image_path IS NULL OR image_path = '')
	           AND name IS NOT NULL AND name <> ''
	         ORDER BY name`
	args := []any{}
	if limit > 0 {
		sql += " LIMIT $1"
		args = append(args, limit)
	}
	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []PersonRow
	for rows.Next() {
		var p PersonRow
		if err := rows.Scan(&p.ID, &p.Name); err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

func (r *PersonRepository) GetByName(ctx context.Context, name string) (*PersonRow, error) {
	p, err := scanPersonRow(r.pool.QueryRow(ctx, `SELECT `+personColumns+` FROM persons WHERE name = $1 LIMIT 1`, name))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return p, err
}

func (r *PersonRepository) GetByID(ctx context.Context, id string) (*PersonRow, error) {
	p, err := scanPersonRow(r.pool.QueryRow(ctx, `SELECT `+personColumns+` FROM persons WHERE id = $1::uuid LIMIT 1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return p, err
}

func (r *PersonRepository) UpdateMetadata(ctx context.Context, id string, u PersonMetadataUpdate) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE persons
		    SET overview              = COALESCE($2, overview),
		        premiere_date         = COALESCE($3, premiere_date),
		        production_year       = COALESCE($4, production_year),
		        tmdb_person_id        = COALESCE($5, tmdb_person_id),
		        production_locations  = COALESCE($6::jsonb, production_locations),
		        genres                = COALESCE($7::jsonb, genres),
		        tags                  = COALESCE($8::jsonb, tags),
		        taglines              = COALESCE($9::jsonb, taglines),
		        provider_ids          = COALESCE($10::jsonb, provider_ids),
		        updated_at            = NOW()
		  WHERE id = $1::uuid`,
		id, u.Overview, u.PremiereDate, u.ProductionYear, u.TmdbPersonID,
		u.ProductionLocations, u.Genres, u.Tags, u.Taglines, u.ProviderIDs)
	return err
}

func (r *PersonRepository) List(ctx context.Context, opts PersonListOptions) ([]PersonRow, int64, error) {
	var total int64
	args := []any{}
	conds := []string{}
	join := ""
	if personFavoriteOnly(opts.Filters) {
		if strings.TrimSpace(opts.UserID) == "" {
			return []PersonRow{}, 0, nil
		}
		args = append(args, opts.UserID)
		join = ` JOIN user_person_data upd
		           ON upd.person_id = p.id
		          AND upd.user_id = $` + strconv.Itoa(len(args)) + `::uuid
		          AND upd.is_favorite = TRUE`
	}
	if opts.Search != "" {
		args = append(args, "%"+opts.Search+"%")
		conds = append(conds, "p.name ILIKE $"+strconv.Itoa(len(args)))
	}
	if opts.NameStartsWith != "" {
		args = append(args, opts.NameStartsWith+"%")
		conds = append(conds, "p.name ILIKE $"+strconv.Itoa(len(args)))
	}
	where := ""
	if len(conds) > 0 {
		where = " WHERE " + strings.Join(conds, " AND ")
	}
	from := ` FROM persons p` + join
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*)`+from+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	listSQL := `SELECT ` + personColumnsP + from + where + ` ORDER BY p.name`
	listArgs := append([]any{}, args...)
	if opts.Limit > 0 {
		listSQL += " LIMIT $" + strconv.Itoa(len(listArgs)+1)
		listArgs = append(listArgs, opts.Limit)
	}
	listSQL += " OFFSET $" + strconv.Itoa(len(listArgs)+1)
	listArgs = append(listArgs, opts.Offset)
	rows, err := r.pool.Query(ctx, listSQL, listArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var result []PersonRow
	for rows.Next() {
		p, err := scanPersonRow(rows)
		if err != nil {
			return nil, 0, err
		}
		result = append(result, *p)
	}
	return result, total, rows.Err()
}

func (r *PersonRepository) ListActorsAdmin(ctx context.Context, f ActorAdminFilter) ([]ActorAdminRow, int64, error) {
	args := []any{}
	conds := []string{}
	if s := strings.TrimSpace(f.Search); s != "" {
		args = append(args, "%"+s+"%")
		conds = append(conds, "p.name ILIKE $"+strconv.Itoa(len(args)))
	}
	switch f.Filter {
	case "missing_image":
		conds = append(conds, "(p.image_path IS NULL OR p.image_path = '')")
	case "has_image":
		conds = append(conds, "(p.image_path IS NOT NULL AND p.image_path <> '')")
	case "locked":
		conds = append(conds, "p.image_locked")
	case "with_works":
		conds = append(conds, "COALESCE(w.cnt, 0) > 0")
	case "junk":
		conds = append(conds, "("+junkNameCond+")")
	}
	where := ""
	if len(conds) > 0 {
		where = " WHERE " + strings.Join(conds, " AND ")
	}
	var total int64
	if err := r.pool.QueryRow(ctx, "SELECT COUNT(*) "+actorAdminFrom+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	listArgs := append([]any{}, args...)
	limClause := ""
	if f.Limit > 0 {
		listArgs = append(listArgs, f.Limit)
		limClause += " LIMIT $" + strconv.Itoa(len(listArgs))
	}
	listArgs = append(listArgs, f.Offset)
	limClause += " OFFSET $" + strconv.Itoa(len(listArgs))
	sql := `SELECT p.id::text, p.name,
	        (p.image_path IS NOT NULL AND p.image_path <> ''),
	        (p.backdrop_path IS NOT NULL AND p.backdrop_path <> ''),
	        p.image_locked,
	        (p.overview IS NOT NULL AND p.overview <> ''),
	        (SELECT COUNT(*) FROM jsonb_object_keys(p.provider_ids))::int,
	        COALESCE(jsonb_array_length(p.tags), 0),
	        COALESCE(w.cnt, 0),
	        (` + junkNameCond + `),
	        EXTRACT(EPOCH FROM p.updated_at)::bigint::text
	        ` + actorAdminFrom + where + actorAdminOrderBy(f.Sort, f.Order) + limClause
	rows, err := r.pool.Query(ctx, sql, listArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]ActorAdminRow, 0, 50)
	for rows.Next() {
		var row ActorAdminRow
		if err := rows.Scan(&row.ID, &row.Name, &row.HasImage, &row.HasBackdrop, &row.ImageLocked,
			&row.HasOverview, &row.ProviderCount, &row.TagCount, &row.WorkCount, &row.IsJunk, &row.ImageTag); err != nil {
			return nil, 0, err
		}
		out = append(out, row)
	}
	return out, total, rows.Err()
}

func (r *PersonRepository) CountWorks(ctx context.Context, personID string) (int64, error) {
	var n int64
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM cast_members WHERE person_id = $1::uuid`, personID).Scan(&n)
	return n, err
}

func (r *PersonRepository) SetImageLocked(ctx context.Context, personID string, locked bool) error {
	_, err := r.pool.Exec(ctx, `UPDATE persons SET image_locked = $2, updated_at = NOW() WHERE id = $1::uuid`, personID, locked)
	return err
}

func (r *PersonRepository) DeletePersons(ctx context.Context, ids []string) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	paths, err := collectPersonImagePaths(ctx, tx, `SELECT image_path, backdrop_path FROM persons WHERE id = ANY($1::uuid[])`, ids)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE cast_members SET person_id = NULL WHERE person_id = ANY($1::uuid[])`, ids); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM persons WHERE id = ANY($1::uuid[])`, ids); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return paths, nil
}

func (r *PersonRepository) DeleteJunkPersons(ctx context.Context) ([]string, int64, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback(ctx)
	paths, err := collectPersonImagePaths(ctx, tx, `SELECT image_path, backdrop_path FROM persons WHERE `+junkNameCondBare)
	if err != nil {
		return nil, 0, err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE cast_members SET person_id = NULL
		   WHERE person_id IN (SELECT id FROM persons WHERE `+junkNameCondBare+`)`); err != nil {
		return nil, 0, err
	}
	ct, err := tx.Exec(ctx, `DELETE FROM persons WHERE `+junkNameCondBare)
	if err != nil {
		return nil, 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, 0, err
	}
	return paths, ct.RowsAffected(), nil
}

func scanPersonRow(row pgx.Row) (*PersonRow, error) {
	var p PersonRow
	if err := row.Scan(
		&p.ID, &p.Name, &p.ImagePath, &p.ImageLocked, &p.TmdbPersonID, &p.Overview, &p.ImageTag,
		&p.PremiereDate, &p.ProductionYear,
		&p.ProductionLocations, &p.Genres, &p.Tags, &p.Taglines, &p.ProviderIDs, &p.BackdropPath,
	); err != nil {
		return nil, err
	}
	return &p, nil
}

func personFavoriteOnly(filters []string) bool {
	for _, f := range filters {
		if strings.EqualFold(strings.TrimSpace(f), "IsFavorite") {
			return true
		}
	}
	return false
}

func actorAdminOrderBy(sort, order string) string {
	dir := "ASC"
	if strings.EqualFold(order, "desc") {
		dir = "DESC"
	}
	switch sort {
	case "works":
		return " ORDER BY COALESCE(w.cnt, 0) " + dir + ", p.name ASC"
	case "updated":
		return " ORDER BY p.updated_at " + dir + ", p.name ASC"
	default:
		return " ORDER BY p.name " + dir
	}
}

func collectPersonImagePaths(ctx context.Context, tx pgx.Tx, sql string, args ...any) ([]string, error) {
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var paths []string
	for rows.Next() {
		var img, bd *string
		if err := rows.Scan(&img, &bd); err != nil {
			return nil, err
		}
		if img != nil && *img != "" {
			paths = append(paths, *img)
		}
		if bd != nil && *bd != "" {
			paths = append(paths, *bd)
		}
	}
	return paths, rows.Err()
}
