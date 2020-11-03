import 'dotenv/config';
import RootRouter from './routes/RootRouter';
import express, { json } from 'express';
import { connect } from 'mongoose';
const app = express();
const port = process.env.PORT || 3000;

app.set('view engine', 'ejs');
app.disable('x-powered-by');
app.use(json());
app.use('/', RootRouter);

app.listen(port, () => {
    console.log(`Listening to port ${port}`);

    connect(process.env.MONGO_URI, {
        useNewUrlParser: true,
        useUnifiedTopology: true,
    }, () => {
        console.log('Connected to the database.');
    });
});
