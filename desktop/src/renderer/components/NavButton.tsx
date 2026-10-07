import { useRef, type ComponentType, type RefAttributes } from "react";

export interface AnimatedIconHandle {
  startAnimation: () => void;
  stopAnimation: () => void;
}

export type AnimatedIcon = ComponentType<{ size?: number; className?: string } & RefAttributes<AnimatedIconHandle>>;

interface NavButtonProps {
  icon: AnimatedIcon;
  label: string;
  active: boolean;
  onClick: () => void;
  testId: string;
}

/** Menü düğmesi: düğmenin tamamına gelince (ya da odaklanınca) ikon hareket eder. */
export function NavButton({ icon: Icon, label, active, onClick, testId }: NavButtonProps) {
  const ref = useRef<AnimatedIconHandle>(null);
  return (
    <button
      type="button"
      className={active ? "nav-item is-active" : "nav-item"}
      aria-current={active ? "page" : undefined}
      data-testid={testId}
      onClick={onClick}
      onMouseEnter={() => ref.current?.startAnimation()}
      onMouseLeave={() => ref.current?.stopAnimation()}
      onFocus={() => ref.current?.startAnimation()}
      onBlur={() => ref.current?.stopAnimation()}
    >
      <Icon ref={ref} size={18} className="nav-icon" />
      <span>{label}</span>
    </button>
  );
}
