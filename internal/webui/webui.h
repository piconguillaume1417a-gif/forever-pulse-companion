#ifndef FPC_WEBUI_H
#define FPC_WEBUI_H

#include <gtk/gtk.h>
#include <webkit2/webkit2.h>

typedef struct FpcView FpcView;

FpcView *fpc_view_new(const char *title, int width, int height, const char *html, int dark);
void fpc_view_free(FpcView *v);
void fpc_show(FpcView *v);
void fpc_hide(FpcView *v);
void fpc_present(FpcView *v);
void fpc_iconify(FpcView *v);
void fpc_resize(FpcView *v, int width, int height);
void fpc_run(FpcView *v);
void fpc_quit(FpcView *v);
void fpc_eval(FpcView *v, const char *js);
void fpc_dispatch(FpcView *v, guint64 id);

char *fpc_choose_folder(FpcView *v, const char *title, const char *initial);
int fpc_confirm(FpcView *v, const char *text, const char *action);
void fpc_info(FpcView *v, const char *text);
void fpc_set_icon(FpcView *v, const unsigned char *data, unsigned long len);
void fpc_free_string(char *s);

/* Implemented in Go. */
extern void goWebuiMessage(char *message);
extern void goWebuiLoadFinished(void);
extern int goWebuiCloseRequested(void);
extern void goWebuiDispatch(guint64 id);

#endif
