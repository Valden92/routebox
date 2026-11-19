/** Выполнить очистку текущего окна ровно один раз при закрытии или замене. */
export class ModalLifecycle {
  private onClose: (() => void) | null = null;

  replace(onClose?: () => void): void {
    this.close();
    this.onClose = onClose ?? null;
  }

  close(): void {
    const onClose = this.onClose;
    this.onClose = null;
    onClose?.();
  }
}
