export function Badge(props: { children: string; variant?: "success" | "danger" | "neutral" }) {
  const cls =
    props.variant === "success" ? "badge badge-success" :
    props.variant === "danger" ? "badge badge-danger" :
    "badge badge-neutral";
  return <span class={cls}>{props.children}</span>;
}
