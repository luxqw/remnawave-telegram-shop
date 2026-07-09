export function Pagination(props: {
  page: number;
  limit: number;
  total: number;
  onChange: (page: number) => void;
}) {
  const pageCount = Math.max(1, Math.ceil(props.total / props.limit));
  return (
    <div class="pagination">
      <span>
        Стр. {props.page} из {pageCount} · всего {props.total}
      </span>
      <button
        class="btn btn-sm btn-ghost"
        disabled={props.page <= 1}
        onClick={() => props.onChange(props.page - 1)}
      >
        Назад
      </button>
      <button
        class="btn btn-sm btn-ghost"
        disabled={props.page >= pageCount}
        onClick={() => props.onChange(props.page + 1)}
      >
        Вперёд
      </button>
    </div>
  );
}
