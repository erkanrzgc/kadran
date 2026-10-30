/** Renkli camın zemini; CSS yalnız `glass` modunda gösterir. */
export function Backdrop() {
  return (
    <div className="backdrop" aria-hidden="true">
      <i className="blob-red" />
      <i className="blob-blue" />
      <i className="blob-amber" />
      <i className="blob-cyan" />
      <div className="stripe" />
    </div>
  );
}
